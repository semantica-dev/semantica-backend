// File: cmd/worker-crawler/main.go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/worker/crawler"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage"
)

func main() {
	cfg := config.LoadConfig()
	serviceName := "worker-crawler-service"
	appLogger := logger.New(serviceName, cfg.LogFormat, cfg.GetSlogLevel())
	appLogger.Info("Starting service...", "name", serviceName)
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"minio_endpoint", cfg.MinioEndpoint,
		"minio_bucket", cfg.MinioBucketName,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	var minioClient *storage.MinioClient
	var minioErr error
	if cfg.MinioEndpoint != "" && cfg.MinioAccessKeyID != "" && cfg.MinioSecretAccessKey != "" && cfg.MinioBucketName != "" {
		minioInternalCfg := storage.MinioConfig{
			Endpoint: cfg.MinioEndpoint, AccessKeyID: cfg.MinioAccessKeyID,
			SecretAccessKey: cfg.MinioSecretAccessKey, UseSSL: cfg.MinioUseSSL,
			BucketName: cfg.MinioBucketName,
		}
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer minioInitCancel()
		minioClient, minioErr = storage.NewMinioClient(minioInitCtx, minioInternalCfg, appLogger.With("component", "minio_client_init"))
		if minioErr != nil {
			appLogger.Error("Failed to initialize Minio client. Exiting.", "error", minioErr)
			os.Exit(1)
		}
		appLogger.Info("Minio client initialized.", "bucket", cfg.MinioBucketName)
	} else {
		appLogger.Error("Minio configuration incomplete. Exiting.", "error", "incomplete Minio config")
		os.Exit(1)
	}

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

	crawlService := crawler.NewCrawlService(appLogger, rmqClient, minioClient)

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

	appLogger.Info("Starting consumer...", "service", serviceName)
	wg.Add(1)
	go startSingleConsumer(
		rmqClient,
		messaging.ConsumeOpts{
			QueueName:    "tasks.crawl.in.queue",
			ExchangeName: messaging.TasksExchange,
			RoutingKey:   messaging.CrawlTaskRoutingKey,
			ConsumerTag:  serviceName + "-consumer-1",
		},
		crawlService.HandleTask,
		appLogger,
	)

	appLogger.Info("Service is now running. Press CTRL+C to exit.", "name", serviceName)
	sig := <-shutdownSignal
	appLogger.Info("Shutdown signal received, initiating graceful shutdown", "signal", sig.String(), "service", serviceName)

	appLogger.Info("Closing RabbitMQ client connection to signal consumer to stop...", "service", serviceName)
	rmqClient.Close()

	appLogger.Info("Waiting for consumer to finish (up to 5s)...", "service", serviceName)
	waitGroupDone := make(chan struct{})
	go func() {
		defer close(waitGroupDone)
		wg.Wait()
	}()

	select {
	case <-waitGroupDone:
		appLogger.Info("Consumer finished gracefully.", "service", serviceName)
	case <-time.After(5 * time.Second):
		appLogger.Warn("Timeout waiting for consumer to finish. Proceeding with shutdown.", "service", serviceName)
	}

	appLogger.Info("Service shut down gracefully.", "name", serviceName)
}
