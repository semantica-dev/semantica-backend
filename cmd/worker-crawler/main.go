// File: cmd/worker-crawler/main.go
package main

import (
	"context" // <--- ДОБАВЛЕНО для Minio
	"os"
	"os/signal"
	"syscall"
	"time" // <--- ДОБАВЛЕНО для Minio

	"github.com/semantica-dev/semantica-backend/internal/worker/crawler"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage" // <--- ДОБАВЛЕНО
)

func main() {
	cfg := config.LoadConfig()

	appLogger := logger.New("worker-crawler-service", cfg.LogFormat, cfg.GetSlogLevel())
	appLogger.Info("Starting Worker-Crawler service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"minio_endpoint", cfg.MinioEndpoint, // <--- ДОБАВЛЕНО логирование Minio конфига
		"minio_bucket", cfg.MinioBucketName, // <--- ДОБАВЛЕНО логирование Minio конфига
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	// --- Инициализация Minio клиента ---
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
		// Контекст с таймаутом для инициализации Minio
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 30*time.Second) // Увеличим немного таймаут
		defer minioInitCancel()

		minioClient, minioErr = storage.NewMinioClient(minioInitCtx, minioInternalCfg, appLogger.With("component", "minio_client_init"))
		if minioErr != nil {
			appLogger.Error("Failed to initialize Minio client or ensure bucket exists. Exiting.", "error", minioErr)
			os.Exit(1) // Критическая ошибка, выходим
		}
		appLogger.Info("Minio client initialized and bucket ensured.", "bucket", cfg.MinioBucketName)
	} else {
		appLogger.Error("Minio configuration is incomplete. Worker-Crawler cannot function without Minio. Exiting.", "error", "incomplete Minio config")
		os.Exit(1) // Краулеру Minio нужен обязательно
	}
	// --- Конец инициализации Minio клиента ---

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
	defer rmqClient.Close()

	// Передаем minioClient в NewCrawlService
	crawlService := crawler.NewCrawlService(appLogger, rmqClient, minioClient) // <--- ИЗМЕНЕНО

	consumeOpts := messaging.ConsumeOpts{
		QueueName:    "tasks.crawl.in.queue",
		ExchangeName: messaging.TasksExchange,
		RoutingKey:   messaging.CrawlTaskRoutingKey,
		ConsumerTag:  "crawler-consumer-1",
	}

	appLogger.Info("Setting up consumer...", "queue", consumeOpts.QueueName, "routing_key", consumeOpts.RoutingKey)
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := rmqClient.Consume(consumeOpts, crawlService.HandleTask); err != nil {
			appLogger.Error("RabbitMQ consumer failed and stopped.", "error", err)
			select {
			case done <- syscall.SIGABRT:
			default:
			}
		}
	}()

	appLogger.Info("Worker-Crawler service is now running. Press CTRL+C to exit.")
	sig := <-done
	appLogger.Info("Received signal, shutting down Worker-Crawler service...", "signal", sig.String())
	appLogger.Info("Worker-Crawler service shut down.")
}
