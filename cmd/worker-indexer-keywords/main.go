// File: cmd/worker-indexer-keywords/main.go
package main

import (
	"context" // Добавлено для Minio
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time" // Добавлено для Minio

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/worker/indexerkeywords"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage" // Добавлено для Minio
)

func main() {
	cfg := config.LoadConfig()
	appLogger := logger.New("worker-indexer-keywords-service", cfg.LogFormat, cfg.GetSlogLevel())
	appLogger.Info("Starting Worker-Indexer-Keywords service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"minio_endpoint", cfg.MinioEndpoint, // Этот воркер будет работать с Minio
		"minio_bucket", cfg.MinioBucketName,
		"postgres_dsn_set", cfg.PostgresDSN != "", // Этот воркер будет работать с PostgreSQL
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	// Инициализация Minio клиента
	var minioClient *storage.MinioClient
	var minioErr error
	if cfg.MinioEndpoint != "" && cfg.MinioAccessKeyID != "" && cfg.MinioSecretAccessKey != "" && cfg.MinioBucketName != "" {
		minioInternalCfg := storage.MinioConfig{
			Endpoint:        cfg.MinioEndpoint,
			AccessKeyID:     cfg.MinioAccessKeyID,
			SecretAccessKey: cfg.MinioSecretAccessKey,
			UseSSL:          cfg.MinioUseSSL,
			BucketName:      cfg.MinioBucketName,
		}
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer minioInitCancel()
		minioClient, minioErr = storage.NewMinioClient(minioInitCtx, minioInternalCfg, appLogger.With("component", "minio_client_init"))
		if minioErr != nil {
			appLogger.Error("Failed to initialize Minio client or ensure bucket exists. Exiting.", "error", minioErr)
			os.Exit(1)
		}
		appLogger.Info("Minio client initialized and bucket ensured.", "bucket", cfg.MinioBucketName)
	} else {
		appLogger.Error("Minio configuration is incomplete. Worker-Indexer-Keywords cannot function without Minio. Exiting.", "error", "incomplete Minio config")
		os.Exit(1)
	}

	// TODO: Инициализация клиента PostgreSQL, когда он понадобится
	// if cfg.PostgresDSN == "" {
	// 	appLogger.Error("POSTGRES_DSN is not set. This worker may need it soon. Continuing for now.", "error", "missing postgres dsn")
	// }

	rmqClient, err := messaging.NewRabbitMQClient(
		cfg.RabbitMQ_URL,
		appLogger.With("component", "rabbitmq_client_setup"),
		cfg.MaxRetries,
		cfg.RetryInterval,
	)
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client. Exiting.", "error", err)
		os.Exit(1)
	}
	// defer rmqClient.Close()

	service := indexerkeywords.NewIndexerKeywordsService(appLogger, rmqClient, minioClient) // Передаем minioClient

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	startSingleConsumer := func(
		client *messaging.RabbitMQClient,
		opts messaging.ConsumeOpts,
		handler func(delivery amqp091.Delivery) error,
		consumerLogger *slog.Logger,
	) {
		defer wg.Done()
		logger := consumerLogger.With("queue", opts.QueueName, "consumer_tag", opts.ConsumerTag)
		for {
			logger.Info("Attempting to start consumer...")
			consumeErr := client.Consume(opts, handler)
			logger.Info("Consumer has stopped.", "error_if_any", consumeErr)
			select {
			case <-shutdownSignal:
				logger.Info("Shutdown signal received. Exiting consumer loop.")
				return
			default:
			}
			if consumeErr != nil {
				logger.Error("Consumer failed. Will attempt to restart after a delay.", "error", consumeErr)
			} else {
				logger.Info("Consumer exited gracefully or connection was closed externally. Checking for shutdown signal before potential restart.")
			}
			select {
			case <-shutdownSignal:
				logger.Info("Shutdown signal received during restart delay. Exiting consumer loop.")
				return
			case <-time.After(cfg.RetryInterval):
				logger.Info("Delay finished. Proceeding to restart consumer.")
			}
		}
	}

	appLogger.Info("Starting Worker-Indexer-Keywords consumer...")
	wg.Add(1)
	go startSingleConsumer(
		rmqClient,
		messaging.ConsumeOpts{
			QueueName:    "tasks.index.keywords.in.queue",
			ExchangeName: messaging.TasksExchange,
			RoutingKey:   messaging.IndexKeywordsTaskRoutingKey,
			ConsumerTag:  "indexer-keywords-consumer-1",
		},
		service.HandleTask,
		appLogger,
	)

	appLogger.Info("Worker-Indexer-Keywords service is now running. Press CTRL+C to exit.")
	sig := <-shutdownSignal
	appLogger.Info("Shutdown signal received, initiating graceful shutdown", "signal", sig.String())

	appLogger.Info("Closing RabbitMQ client connection to signal consumer to stop...")
	rmqClient.Close()

	appLogger.Info("Waiting for consumer to finish...")
	wg.Wait()

	appLogger.Info("Worker-Indexer-Keywords service shut down gracefully.")
}
