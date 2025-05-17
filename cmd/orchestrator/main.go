// File: cmd/orchestrator/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	// Драйвер PostgreSQL должен быть импортирован для регистрации в database/sql.
	_ "github.com/lib/pq"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/api"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/listener"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/config" // Используем наш обновленный пакет config
	"github.com/semantica-dev/semantica-backend/pkg/database"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage"
)

func main() {
	// 1. Загружаем конфигурацию в самом начале
	cfg := config.LoadConfig()

	// 2. Настраиваем логгер (можно будет добавить установку уровня из cfg.LogLevel)
	appLogger := logger.New("orchestrator-service")
	appLogger.Info("Starting Orchestrator service...")
	// Логируем важные части конфигурации для отладки (не весь cfg, чтобы не светить пароли в проде)
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"orchestrator_api_port", cfg.OrchestratorAPIPort,
		"postgres_dsn_set", cfg.PostgresDSN != "", // Показываем, задан ли DSN, а не сам DSN
		"minio_endpoint", cfg.MinioEndpoint,
		"minio_bucket", cfg.MinioBucketName,
		"migrations_dir", cfg.MigrationsDir,
		"log_level", cfg.LogLevel,
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	// 3. Применение миграций базы данных
	if cfg.PostgresDSN != "" {
		// Путь к миграциям теперь берем из cfg.MigrationsDir
		// В Dockerfile Оркестратора мы копируем ./db в /app/db,
		// поэтому MIGRATIONS_DIR в docker-compose.yaml для Оркестратора должен быть /app/db/migrations
		// (это значение по умолчанию в pkg/config, если не переопределено в .env)
		if err := database.ApplyMigrations(appLogger, "postgres", cfg.PostgresDSN, cfg.MigrationsDir); err != nil {
			appLogger.Error("Database migration failed. Exiting.", "error", err)
			os.Exit(1)
		}
	} else {
		appLogger.Warn("POSTGRES_DSN is not set. Orchestrator requires a database for storing task states and will likely fail or not function correctly.")
		// os.Exit(1) // Раскомментировать, если БД критична для старта и DSN должен быть всегда
	}

	// 4. Инициализация Minio клиента и создание/проверка бакета
	if cfg.MinioEndpoint != "" && cfg.MinioAccessKeyID != "" && cfg.MinioSecretAccessKey != "" && cfg.MinioBucketName != "" {
		minioInternalCfg := storage.MinioConfig{
			Endpoint:        cfg.MinioEndpoint,
			AccessKeyID:     cfg.MinioAccessKeyID,
			SecretAccessKey: cfg.MinioSecretAccessKey,
			UseSSL:          cfg.MinioUseSSL,
			BucketName:      cfg.MinioBucketName,
		}
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer minioInitCancel()

		_, err := storage.NewMinioClient(minioInitCtx, minioInternalCfg, appLogger)
		if err != nil {
			appLogger.Error("Failed to initialize Minio client or ensure bucket exists. Exiting.", "error", err)
			os.Exit(1)
		}
		appLogger.Info("Minio client initialized and bucket ensured.", "bucket", cfg.MinioBucketName)
	} else {
		appLogger.Warn("Minio configuration is incomplete. Minio-dependent features may fail.")
		// os.Exit(1); // Раскомментировать, если Minio критичен для старта
	}

	// 5. Инициализация RabbitMQ клиента
	rmqClient, err := messaging.NewRabbitMQClient(
		cfg.RabbitMQ_URL,
		appLogger.With("component", "rabbitmq_client"),
		cfg.MaxRetries,
		cfg.RetryInterval,
	)
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client after all retries. Exiting.", "error", err)
		os.Exit(1)
	}
	// defer rmqClient.Close() // Закрываем явно в конце main

	// 6. Инициализация компонентов Оркестратора
	taskPublisher := publisher.NewTaskPublisher(rmqClient, appLogger)
	taskAPIHandler := api.NewTaskAPIHandler(appLogger, taskPublisher)
	taskListener := listener.NewTaskListener(appLogger, taskPublisher)

	// 7. Настройка HTTP сервера
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/crawl", taskAPIHandler.CreateCrawlTaskHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.OrchestratorAPIPort, // Используем порт из конфигурации
		Handler: mux,
	}

	// 8. Запуск слушателей RabbitMQ (консьюмеров)
	var wg sync.WaitGroup
	startConsumer := func(consumerOpts messaging.ConsumeOpts, handlerFunc func(delivery amqp091.Delivery) error) {
		defer wg.Done()
		appLogger.Info("Orchestrator starting consumer",
			"queue", consumerOpts.QueueName,
			"routing_key", consumerOpts.RoutingKey,
			"consumer_tag", consumerOpts.ConsumerTag)

		if err := rmqClient.Consume(consumerOpts, handlerFunc); err != nil {
			appLogger.Error("Orchestrator consumer exited with error",
				"queue", consumerOpts.QueueName,
				"consumer_tag", consumerOpts.ConsumerTag,
				"error", err)
		}
		appLogger.Info("Orchestrator consumer stopped",
			"queue", consumerOpts.QueueName,
			"consumer_tag", consumerOpts.ConsumerTag)
	}

	consumers := []struct {
		opts    messaging.ConsumeOpts
		handler func(delivery amqp091.Delivery) error
	}{
		{messaging.ConsumeOpts{QueueName: "orchestrator.crawl.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.CrawlResultRoutingKey, ConsumerTag: "orchestrator-crawl-result-consumer"}, taskListener.HandleCrawlResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.extract_html.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractHTMLResultRoutingKey, ConsumerTag: "orchestrator-extract-html-result-consumer"}, taskListener.HandleExtractHTMLResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_keywords.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexKeywordsResultRoutingKey, ConsumerTag: "orchestrator-index-keywords-result-consumer"}, taskListener.HandleIndexKeywordsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_embeddings.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexEmbeddingsResultRoutingKey, ConsumerTag: "orchestrator-index-embeddings-result-consumer"}, taskListener.HandleIndexEmbeddingsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.task_finished.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.TaskProcessingFinishedRoutingKey, ConsumerTag: "orchestrator-task-finished-consumer"}, taskListener.HandleTaskProcessingFinished},
	}

	for _, c := range consumers {
		wg.Add(1)
		go startConsumer(c.opts, c.handler)
	}

	// 9. Настройка грациозного завершения
	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)
	httpServerDone := make(chan struct{})

	go func() {
		appLogger.Info("Orchestrator API server starting", "port", cfg.OrchestratorAPIPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Could not listen on HTTP port", "port", cfg.OrchestratorAPIPort, "error", err)
			select {
			case shutdownSignal <- syscall.SIGTERM:
			default:
			}
		}
		close(httpServerDone)
	}()

	sig := <-shutdownSignal
	appLogger.Info("Shutdown signal received.", "signal", sig.String(), "Initiating graceful shutdown...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	appLogger.Info("Attempting to shut down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Warn("HTTP Server shutdown error (or already stopped)", "error", err)
	}

	<-httpServerDone
	appLogger.Info("HTTP Server stopped.")

	appLogger.Info("Closing RabbitMQ connection to stop consumers...")
	rmqClient.Close()

	appLogger.Info("Waiting for all RabbitMQ consumers to finish...")
	wg.Wait()

	appLogger.Info("Orchestrator service shut down gracefully.")
}
