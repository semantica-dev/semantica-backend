// File: cmd/orchestrator/main.go
package main

import (
	"context"
	"fmt"
	"io"
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
	// 1. Загружаем конфигурацию в самом начале
	cfg := config.LoadConfig()

	// 2. Настраиваем логгер с использованием формата и уровня из конфигурации
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

	// 3. Применение миграций базы данных
	if cfg.PostgresDSN != "" {
		if err := database.ApplyMigrations(appLogger, "postgres", cfg.PostgresDSN, cfg.MigrationsDir); err != nil {
			appLogger.Error("Database migration failed. Exiting.", "error", err)
			os.Exit(1)
		}
	} else {
		appLogger.Warn("POSTGRES_DSN is not set. Orchestrator requires a database for storing task states and will likely fail or not function correctly.")
	}

	// 4. Инициализация Minio клиента и создание/проверка бакета
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
		minioInitCtx, minioInitCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer minioInitCancel()

		minioClient, minioErr = storage.NewMinioClient(minioInitCtx, minioInternalCfg, appLogger)
		if minioErr != nil {
			appLogger.Error("Failed to initialize Minio client or ensure bucket exists. Exiting.", "error", minioErr)
			os.Exit(1)
		}
		appLogger.Info("Minio client initialized and bucket ensured.", "bucket", cfg.MinioBucketName)
	} else {
		appLogger.Warn("Minio configuration is incomplete. Minio-dependent features may fail.")
		minioErr = fmt.Errorf("minio configuration incomplete")
	}

	// --- TEMPORARY MINIO CLIENT TEST (Upload and Get) ---
	if minioErr == nil && minioClient != nil {
		appLogger.Info("[TEMP_TEST] Starting Minio client operational test...")
		testObjectName := "test/orchestrator_startup_check.txt"
		testContent := "Minio client test from Orchestrator: OK at " + time.Now().Format(time.RFC3339Nano)
		testContentType := "text/plain"
		testTimeout := 10 * time.Second

		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), testTimeout)
		defer uploadCancel()

		reader := strings.NewReader(testContent)
		contentLength := int64(len(testContent))

		appLogger.Info("[TEMP_TEST] Attempting to upload test object...", "bucket", minioClient.GetBucketName(), "object", testObjectName)
		_, err := minioClient.UploadObject(uploadCtx, testObjectName, reader, contentLength, testContentType)

		if err != nil {
			appLogger.Error("[TEMP_TEST] Failed to upload test object", "object", testObjectName, "error", err)
		} else {
			appLogger.Info("[TEMP_TEST] Test object uploaded successfully", "object", testObjectName)

			getCtx, getCancel := context.WithTimeout(context.Background(), testTimeout)
			defer getCancel()

			appLogger.Info("[TEMP_TEST] Attempting to get test object...", "object", testObjectName)
			obj, errGet := minioClient.GetObject(getCtx, testObjectName)

			if errGet != nil {
				appLogger.Error("[TEMP_TEST] Failed to get test object", "object", testObjectName, "error", errGet)
			} else {
				defer func() {
					if errClose := obj.Close(); errClose != nil {
						appLogger.Error("[TEMP_TEST] Failed to close Minio object reader", "object", testObjectName, "error", errClose)
					}
				}()

				retrievedBytes, errRead := io.ReadAll(obj)
				if errRead != nil {
					appLogger.Error("[TEMP_TEST] Failed to read test object content", "object", testObjectName, "error", errRead)
				} else {
					retrievedContent := string(retrievedBytes)
					if retrievedContent == testContent {
						appLogger.Info("[TEMP_TEST] Test object retrieved and content verified successfully", "object", testObjectName, "content_length", len(retrievedContent))
					} else {
						expectedPrefix := testContent
						if len(testContent) > 20 {
							expectedPrefix = testContent[:20] + "..."
						}
						retrievedPrefix := retrievedContent
						if len(retrievedContent) > 20 {
							retrievedPrefix = retrievedContent[:20] + "..."
						}
						appLogger.Error("[TEMP_TEST] Test object content mismatch",
							"object", testObjectName,
							"expected_len", len(testContent), "got_len", len(retrievedContent),
							"expected_prefix", expectedPrefix,
							"got_prefix", retrievedPrefix)
					}
				}
			}
		}
		appLogger.Info("[TEMP_TEST] Minio client operational test finished.")
	} else {
		appLogger.Warn("[TEMP_TEST] Skipping Minio client operational test due to initialization error or incomplete config.")
	}
	// --- END TEMPORARY MINIO CLIENT TEST ---

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
		{messaging.ConsumeOpts{QueueName: "orchestrator.extract_other.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractOtherResultRoutingKey, ConsumerTag: "orchestrator-extract-other-result-consumer"}, taskListener.HandleExtractOtherResult},
	}

	for _, c := range consumers {
		wg.Add(1)
		go startConsumer(c.opts, c.handler)
	}

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
	appLogger.Info("Shutdown signal received, initiating graceful shutdown", "signal", sig.String())

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
