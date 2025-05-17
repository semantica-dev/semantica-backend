// File: cmd/worker-indexer-embeddings/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"
	// "time"

	// Драйвер Qdrant или другой векторной БД, если этот воркер будет сам подключаться
	// Пока не нужен, т.к. реальной записи в БД нет

	"github.com/semantica-dev/semantica-backend/internal/worker/indexerembeddings"
	"github.com/semantica-dev/semantica-backend/pkg/config" // Используем наш пакет config
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	// "github.com/semantica-dev/semantica-backend/pkg/vectorstore" // Если будет клиент для Qdrant
)

func main() {
	cfg := config.LoadConfig() // 1. Загружаем конфигурацию

	appLogger := logger.New("worker-indexer-embeddings-service") // 2. Инициализируем логгер
	appLogger.Info("Starting Worker-Indexer-Embeddings service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"minio_endpoint", cfg.MinioEndpoint, // Этот воркер будет читать из Minio
		// "qdrant_url", cfg.QdrantURL, // Когда добавим Qdrant
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	// TODO: Когда будет реальная запись в Qdrant, здесь нужна будет инициализация клиента Qdrant
	// и, возможно, передача его в NewIndexerEmbeddingsService.

	// 3. Используем значения из cfg для RabbitMQ
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

	// 4. Остальная логика
	// Пока NewIndexerEmbeddingsService не принимает клиент Qdrant, но в будущем будет
	service := indexerembeddings.NewIndexerEmbeddingsService(appLogger, rmqClient /*, qdrantClient */)

	consumeOpts := messaging.ConsumeOpts{
		QueueName:    "tasks.index.embeddings.in.queue",
		ExchangeName: messaging.TasksExchange,
		RoutingKey:   messaging.IndexEmbeddingsTaskRoutingKey,
		ConsumerTag:  "indexer-embeddings-consumer-1",
	}

	appLogger.Info("Setting up consumer...", "queue", consumeOpts.QueueName, "routing_key", consumeOpts.RoutingKey)
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := rmqClient.Consume(consumeOpts, service.HandleTask); err != nil {
			appLogger.Error("RabbitMQ consumer failed and stopped.", "error", err)
			select {
			case done <- syscall.SIGABRT:
			default:
			}
		}
	}()

	appLogger.Info("Worker-Indexer-Embeddings service is now running. Press CTRL+C to exit.")
	sig := <-done
	appLogger.Info("Received signal, shutting down Worker-Indexer-Embeddings service...", "signal", sig.String())
	appLogger.Info("Worker-Indexer-Embeddings service shut down.")
}
