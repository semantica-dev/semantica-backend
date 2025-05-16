// File: cmd/orchestrator/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time" // Убедимся, что time импортирован для retryInterval

	// Драйвер PostgreSQL
	_ "github.com/lib/pq"

	"github.com/rabbitmq/amqp091-go" // Явный импорт, если нужен тип в startConsumer
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/api"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/listener"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/database"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	appLogger := logger.New("orchestrator-service")
	appLogger.Info("Starting Orchestrator service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL, "api_port", cfg.Orchestrator_API_Port)

	// --- Применение миграций базы данных ---
	postgresDSN := os.Getenv("POSTGRES_DSN")
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "db/migrations"
		appLogger.Info("MIGRATIONS_DIR not set, using default", "path", migrationsDir)
	}

	if postgresDSN != "" {
		if err := database.ApplyMigrations(appLogger, "postgres", postgresDSN, migrationsDir); err != nil {
			appLogger.Error("Database migration failed. Exiting.", "error", err)
			os.Exit(1)
		}
	} else {
		appLogger.Warn("POSTGRES_DSN is not set. Orchestrator requires a database for storing task states and will likely fail or not function correctly.")
		// Для Оркестратора это критично, можно добавить os.Exit(1)
		// os.Exit(1)
	}
	// --- Миграции применены (или пропущены) ---

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
	// defer rmqClient.Close() // Закрываем явно в конце main

	taskPublisher := publisher.NewTaskPublisher(rmqClient, appLogger)
	taskAPIHandler := api.NewTaskAPIHandler(appLogger, taskPublisher)
	taskListener := listener.NewTaskListener(appLogger, taskPublisher)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/crawl", taskAPIHandler.CreateCrawlTaskHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.Orchestrator_API_Port,
		Handler: mux,
	}

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

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	httpServerDone := make(chan struct{})

	go func() {
		appLogger.Info("Orchestrator API server starting", "port", cfg.Orchestrator_API_Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Could not listen on HTTP port", "port", cfg.Orchestrator_API_Port, "error", err)
			select {
			case shutdownSignal <- syscall.SIGTERM: // Попытка инициировать грациозное завершение
			default: // Если канал уже получил сигнал, ничего не делаем
			}
		}
		close(httpServerDone)
	}()

	<-shutdownSignal
	appLogger.Info("Shutdown signal received. Initiating graceful shutdown...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	appLogger.Info("Attempting to shut down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Error("HTTP Server shutdown error", "error", err)
	}

	<-httpServerDone
	appLogger.Info("HTTP Server stopped.")

	appLogger.Info("Closing RabbitMQ connection to stop consumers...")
	rmqClient.Close()

	appLogger.Info("Waiting for all RabbitMQ consumers to finish...")
	wg.Wait()

	appLogger.Info("Orchestrator service shut down gracefully.")
}
