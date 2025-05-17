// File: cmd/orchestrator/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv" // Для MINIO_USE_SSL
	"sync"
	"syscall"
	"time"

	// Драйвер PostgreSQL должен быть импортирован для регистрации в database/sql.
	_ "github.com/lib/pq"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/api"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/listener"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/database" // Наш пакет для работы с БД, включая миграции
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage" // Наш пакет для работы с Minio
)

func main() {
	appLogger := logger.New("orchestrator-service")
	appLogger.Info("Starting Orchestrator service...")

	cfg := config.LoadConfig() // Загружаем базовый конфиг (порты, URL RabbitMQ)
	appLogger.Info("Base configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL, "api_port", cfg.Orchestrator_API_Port)

	// --- Применение миграций базы данных ---
	postgresDSN := os.Getenv("POSTGRES_DSN")
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "db/migrations" // Значение по умолчанию для локального запуска
		appLogger.Info("MIGRATIONS_DIR not set, using default", "path", migrationsDir)
	}

	if postgresDSN != "" { // Применяем миграции только если DSN задан
		if err := database.ApplyMigrations(appLogger, "postgres", postgresDSN, migrationsDir); err != nil {
			appLogger.Error("Database migration failed. Exiting.", "error", err)
			os.Exit(1) // Критическая ошибка, если миграции не прошли
		}
	} else {
		appLogger.Warn("POSTGRES_DSN is not set. Orchestrator requires a database for storing task states and will likely fail or not function correctly.")
		// В реальном приложении, если Оркестратору критически нужна БД, здесь тоже должен быть os.Exit(1)
		// os.Exit(1)
	}
	// --- Миграции применены (или пропущены) ---

	// --- Инициализация Minio клиента и создание/проверка бакета ---
	minioEndpoint := os.Getenv("MINIO_ENDPOINT")
	minioAccessKey := os.Getenv("MINIO_ACCESS_KEY_ID")
	minioSecretKey := os.Getenv("MINIO_SECRET_ACCESS_KEY")
	minioUseSSLStr := os.Getenv("MINIO_USE_SSL")
	minioBucketName := os.Getenv("MINIO_BUCKET_NAME")

	minioUseSSL, err := strconv.ParseBool(minioUseSSLStr)
	if err != nil {
		appLogger.Warn("Failed to parse MINIO_USE_SSL, defaulting to false", "value", minioUseSSLStr, "error", err)
		minioUseSSL = false
	}

	if minioEndpoint != "" && minioAccessKey != "" && minioSecretKey != "" && minioBucketName != "" {
		minioCfg := storage.MinioConfig{
			Endpoint:        minioEndpoint,
			AccessKeyID:     minioAccessKey,
			SecretAccessKey: minioSecretKey,
			UseSSL:          minioUseSSL,
			BucketName:      minioBucketName,
		}
		// Используем контекст с таймаутом для инициализации Minio
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 60*time.Second) // Увеличим таймаут на всякий случай
		defer minioInitCancel()

		// Инициализируем клиент Minio. Функция NewMinioClient сама проверит/создаст бакет.
		// На данном этапе Оркестратору сам клиент может быть не нужен для операций с объектами,
		// но он выполнит важную роль по созданию бакета.
		_, err := storage.NewMinioClient(minioInitCtx, minioCfg, appLogger)
		if err != nil {
			appLogger.Error("Failed to initialize Minio client or ensure bucket exists. Exiting.", "error", err)
			os.Exit(1)
		}
		appLogger.Info("Minio client initialized and bucket ensured.", "bucket", minioBucketName)
	} else {
		appLogger.Warn("Minio configuration is incomplete. Minio-dependent features may fail.")
		// os.Exit(1); // Раскомментировать, если Minio критичен для старта Оркестратора
	}
	// --- Minio клиент инициализирован ---

	// Параметры для retry подключения к RabbitMQ
	const rabbitMaxRetries = 5
	const rabbitRetryInterval = 5 * time.Second

	rmqClient, err := messaging.NewRabbitMQClient(
		cfg.RabbitMQ_URL,
		appLogger.With("component", "rabbitmq_client"),
		rabbitMaxRetries,
		rabbitRetryInterval,
	)
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client after all retries. Exiting.", "error", err)
		os.Exit(1)
	}
	// defer rmqClient.Close() // Закрываем явно в конце main для корректной работы с wg.Wait()

	// Инициализация компонентов Оркестратора
	// На данном этапе Оркестратору не нужен прямой доступ к *sql.DB или *storage.MinioClient
	// для его основной логики (API, Listener, Publisher), так как он только координирует.
	// Если бы он сам писал в БД или Minio, мы бы передали эти клиенты.
	taskPublisher := publisher.NewTaskPublisher(rmqClient, appLogger)
	taskAPIHandler := api.NewTaskAPIHandler(appLogger, taskPublisher)
	taskListener := listener.NewTaskListener(appLogger, taskPublisher)

	// Настройка HTTP сервера
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/crawl", taskAPIHandler.CreateCrawlTaskHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.Orchestrator_API_Port, // Берется из cfg, который читает ORCHESTRATOR_API_PORT из .env
		Handler: mux,
	}

	// WaitGroup для ожидания завершения всех горутин консьюмеров
	var wg sync.WaitGroup

	// Вспомогательная функция для запуска консьюмеров
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

	// Запуск всех консьюмеров Оркестратора
	consumers := []struct {
		opts    messaging.ConsumeOpts
		handler func(delivery amqp091.Delivery) error
	}{
		{messaging.ConsumeOpts{QueueName: "orchestrator.crawl.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.CrawlResultRoutingKey, ConsumerTag: "orchestrator-crawl-result-consumer"}, taskListener.HandleCrawlResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.extract_html.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractHTMLResultRoutingKey, ConsumerTag: "orchestrator-extract-html-result-consumer"}, taskListener.HandleExtractHTMLResult},
		// {messaging.ConsumeOpts{QueueName: "orchestrator.extract_other.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractOtherResultRoutingKey, ConsumerTag: "orchestrator-extract-other-result-consumer"}, taskListener.HandleExtractOtherResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_keywords.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexKeywordsResultRoutingKey, ConsumerTag: "orchestrator-index-keywords-result-consumer"}, taskListener.HandleIndexKeywordsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_embeddings.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexEmbeddingsResultRoutingKey, ConsumerTag: "orchestrator-index-embeddings-result-consumer"}, taskListener.HandleIndexEmbeddingsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.task_finished.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.TaskProcessingFinishedRoutingKey, ConsumerTag: "orchestrator-task-finished-consumer"}, taskListener.HandleTaskProcessingFinished},
	}

	for _, c := range consumers {
		wg.Add(1)
		go startConsumer(c.opts, c.handler)
	}

	// Канал для обработки сигналов ОС для грациозного завершения
	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	// Канал для сигнализации о завершении HTTP сервера
	httpServerDone := make(chan struct{})

	// Горутина для запуска и остановки HTTP сервера
	go func() {
		appLogger.Info("Orchestrator API server starting", "port", cfg.Orchestrator_API_Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Could not listen on HTTP port", "port", cfg.Orchestrator_API_Port, "error", err)
			// Инициируем остановку всего приложения, если HTTP сервер не может запуститься
			select {
			case shutdownSignal <- syscall.SIGTERM:
			default: // Канал уже может быть закрыт или сигнал уже отправлен
			}
		}
		close(httpServerDone) // Сигнализируем, что HTTP сервер завершил работу (или не смог запуститься)
	}()

	// Ожидаем сигнала на завершение (SIGINT/SIGTERM)
	sig := <-shutdownSignal
	appLogger.Info("Shutdown signal received.", "signal", sig.String(), "Initiating graceful shutdown...")

	// Инициируем остановку HTTP сервера (если он еще работает)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	appLogger.Info("Attempting to shut down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		// Эта ошибка может возникнуть, если сервер уже был остановлен или не смог запуститься
		appLogger.Warn("HTTP Server shutdown error (or already stopped)", "error", err)
	}

	// Ждем фактического завершения HTTP сервера
	<-httpServerDone
	appLogger.Info("HTTP Server stopped.")

	// Закрываем соединение с RabbitMQ, это инициирует остановку консьюмеров.
	appLogger.Info("Closing RabbitMQ connection to stop consumers...")
	rmqClient.Close()

	// Ожидаем завершения всех горутин консьюмеров
	appLogger.Info("Waiting for all RabbitMQ consumers to finish...")
	wg.Wait()

	appLogger.Info("Orchestrator service shut down gracefully.")
}
