// File: cmd/orchestrator/main.go
package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq" // Драйвер PostgreSQL

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/api"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/listener"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/database"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage"
)

func main() {
	cfg := config.LoadConfig()
	appLogger := logger.New("orchestrator-service", cfg.LogFormat, cfg.GetSlogLevel())
	appLogger.Info("Starting Orchestrator service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"orchestrator_api_port", cfg.OrchestratorAPIPort,
		"postgres_dsn_set", cfg.PostgresDSN != "",
		"minio_endpoint", cfg.MinioEndpoint,
		"minio_bucket", cfg.MinioBucketName,
		"migrations_dir", cfg.MigrationsDir,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	if cfg.PostgresDSN != "" {
		if err := database.ApplyMigrations(appLogger, "postgres", cfg.PostgresDSN, cfg.MigrationsDir); err != nil {
			appLogger.Error("Database migration failed. Exiting.", "error", err)
			os.Exit(1)
		}
	} else {
		appLogger.Warn("POSTGRES_DSN is not set. Orchestrator requires a database.")
	}

	var minioClient *storage.MinioClient
	var minioErr error
	if cfg.MinioEndpoint != "" && cfg.MinioAccessKeyID != "" && cfg.MinioSecretAccessKey != "" && cfg.MinioBucketName != "" {
		minioInternalCfg := storage.MinioConfig{
			Endpoint: cfg.MinioEndpoint, AccessKeyID: cfg.MinioAccessKeyID,
			SecretAccessKey: cfg.MinioSecretAccessKey, UseSSL: cfg.MinioUseSSL,
			BucketName: cfg.MinioBucketName,
		}
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer minioInitCancel() // Отменяем контекст при выходе из main
		minioClient, minioErr = storage.NewMinioClient(minioInitCtx, minioInternalCfg, appLogger.With("component", "minio_client_init"))
		if minioErr != nil {
			appLogger.Error("Failed to initialize Minio client or ensure bucket exists. Non-Minio features might still work.", "error", minioErr)
		} else {
			appLogger.Info("Minio client initialized and bucket ensured.", "bucket", cfg.MinioBucketName)
			// --- TEMPORARY MINIO CLIENT TEST (исправленный контекст) ---
			appLogger.Info("[TEMP_TEST] Starting Minio client operational test...")
			testObjectName := "test/orchestrator_startup_check.txt"
			testContent := "Minio client test from Orchestrator: OK at " + time.Now().Format(time.RFC3339Nano)
			testContentType := "text/plain"
			testTimeout := 10 * time.Second // Таймаут на каждую операцию

			uploadCtx, uploadOpCancel := context.WithTimeout(context.Background(), testTimeout)
			reader := strings.NewReader(testContent)
			contentLength := int64(len(testContent))
			_, errUpload := minioClient.UploadObject(uploadCtx, testObjectName, reader, contentLength, testContentType)
			uploadOpCancel() // Отменяем контекст сразу после операции

			if errUpload != nil {
				appLogger.Error("[TEMP_TEST] Failed to upload test object", "object", testObjectName, "error", errUpload)
			} else {
				appLogger.Info("[TEMP_TEST] Test object uploaded successfully", "object", testObjectName)

				getCtx, getOpCancel := context.WithTimeout(context.Background(), testTimeout)
				obj, errGet := minioClient.GetObject(getCtx, testObjectName)
				// Не делаем defer getOpCancel() здесь, если obj.Close() может быть позже

				if errGet != nil {
					getOpCancel() // Отменяем, если GetObject вернул ошибку
					appLogger.Error("[TEMP_TEST] Failed to get test object", "object", testObjectName, "error", errGet)
				} else {
					retrievedBytes, errRead := io.ReadAll(obj)
					obj.Close()   // Закрываем объект сразу
					getOpCancel() // Отменяем контекст после всех операций с ним

					if errRead != nil {
						appLogger.Error("[TEMP_TEST] Failed to read test object content", "object", testObjectName, "error", errRead)
					} else if string(retrievedBytes) == testContent {
						appLogger.Info("[TEMP_TEST] Test object retrieved and content verified successfully", "object", testObjectName)
					} else {
						appLogger.Error("[TEMP_TEST] Test object content mismatch")
					}
				}
			}
			appLogger.Info("[TEMP_TEST] Minio client operational test finished.")
			// --- END TEMPORARY MINIO CLIENT TEST ---
		}
	} else {
		appLogger.Warn("Minio configuration is incomplete. Minio-dependent features may fail.")
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
		Addr:    cfg.OrchestratorAPIPort,
		Handler: mux,
	}

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

	consumersToStart := []struct {
		opts    messaging.ConsumeOpts
		handler func(delivery amqp091.Delivery) error
	}{
		{messaging.ConsumeOpts{QueueName: "orchestrator.crawl.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.CrawlResultRoutingKey, ConsumerTag: "orchestrator-crawl-result-consumer"}, taskListener.HandleCrawlResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.extract_html.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractHTMLResultRoutingKey, ConsumerTag: "orchestrator-extract-html-result-consumer"}, taskListener.HandleExtractHTMLResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.extract_other.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractOtherResultRoutingKey, ConsumerTag: "orchestrator-extract-other-result-consumer"}, taskListener.HandleExtractOtherResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_keywords.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexKeywordsResultRoutingKey, ConsumerTag: "orchestrator-index-keywords-result-consumer"}, taskListener.HandleIndexKeywordsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_embeddings.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexEmbeddingsResultRoutingKey, ConsumerTag: "orchestrator-index-embeddings-result-consumer"}, taskListener.HandleIndexEmbeddingsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.task_finished.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.TaskProcessingFinishedRoutingKey, ConsumerTag: "orchestrator-task-finished-consumer"}, taskListener.HandleTaskProcessingFinished},
	}

	appLogger.Info("Starting Orchestrator consumers...", "count", len(consumersToStart))
	for _, cons := range consumersToStart {
		wg.Add(1)
		go startSingleConsumer(rmqClient, cons.opts, cons.handler, appLogger)
	}

	httpServerDone := make(chan struct{})
	go func() {
		defer close(httpServerDone)
		appLogger.Info("Orchestrator API server starting", "port", cfg.OrchestratorAPIPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Could not listen on HTTP port, signaling shutdown", "port", cfg.OrchestratorAPIPort, "error", err)
			select {
			case shutdownSignal <- syscall.SIGABRT:
			default:
			}
		}
		appLogger.Info("Orchestrator API server stopped.")
	}()

	sig := <-shutdownSignal
	appLogger.Info("Shutdown signal received, initiating graceful shutdown", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second) // Таймаут для остановки HTTP сервера
	defer shutdownCancel()

	appLogger.Info("Attempting to shut down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Warn("HTTP Server shutdown error (or already stopped)", "error", err)
	}
	<-httpServerDone

	appLogger.Info("Closing RabbitMQ client connection to signal consumers to stop...")
	rmqClient.Close()

	appLogger.Info("Waiting for all consumers to finish (up to 5s)...")
	waitGroupDone := make(chan struct{})
	go func() {
		defer close(waitGroupDone)
		wg.Wait()
	}()

	select {
	case <-waitGroupDone:
		appLogger.Info("All consumers finished gracefully.")
	case <-time.After(5 * time.Second):
		appLogger.Warn("Timeout waiting for consumers to finish. Proceeding with shutdown.")
	}

	appLogger.Info("Orchestrator service shut down gracefully.")
}
