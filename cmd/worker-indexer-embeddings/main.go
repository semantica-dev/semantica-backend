// File: cmd/worker-indexer-embeddings/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"
	"time" // Добавляем импорт time

	"github.com/semantica-dev/semantica-backend/internal/worker/indexerembeddings"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	appLogger := logger.New("worker-indexer-embeddings-service")
	appLogger.Info("Starting Worker-Indexer-Embeddings service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL)

	// Параметры для retry подключения к RabbitMQ
	const rabbitMaxRetries = 5
	const rabbitRetryInterval = 5 * time.Second

	rmqClient, err := messaging.NewRabbitMQClient(
		cfg.RabbitMQ_URL,
		appLogger.With("component", "rabbitmq_client"),
		rabbitMaxRetries,    // <--- Добавлен параметр
		rabbitRetryInterval, // <--- Добавлен параметр
	)
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client after all retries. Exiting.", "error", err)
		os.Exit(1)
	}
	defer rmqClient.Close()

	service := indexerembeddings.NewIndexerEmbeddingsService(appLogger, rmqClient)

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
