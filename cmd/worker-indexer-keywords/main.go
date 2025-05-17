// File: cmd/worker-indexer-keywords/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"
	// "time"

	// Драйвер PostgreSQL, если этот воркер будет сам подключаться к БД для записи ключевых слов
	// _ "github.com/lib/pq" // Пока не нужен, т.к. реальной записи в БД нет

	"github.com/semantica-dev/semantica-backend/internal/worker/indexerkeywords"
	"github.com/semantica-dev/semantica-backend/pkg/config" // Используем наш пакет config
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	// "github.com/semantica-dev/semantica-backend/pkg/database" // Если бы он применял свои миграции
)

func main() {
	cfg := config.LoadConfig() // 1. Загружаем конфигурацию

	appLogger := logger.New("worker-indexer-keywords-service") // 2. Инициализируем логгер
	appLogger.Info("Starting Worker-Indexer-Keywords service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"minio_endpoint", cfg.MinioEndpoint, // Этот воркер будет читать из Minio
		"postgres_dsn_set", cfg.PostgresDSN != "", // Этот воркер будет писать в PostgreSQL
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

	// TODO: Когда будет реальная запись в PostgreSQL, здесь нужна будет инициализация соединения с БД
	// и, возможно, передача *sql.DB в NewIndexerKeywordsService.
	// if cfg.PostgresDSN == "" {
	// 	appLogger.Error("POSTGRES_DSN is not set. Indexer-Keywords worker requires a database. Exiting.")
	// 	os.Exit(1)
	// }
	// db, err := sql.Open("postgres", cfg.PostgresDSN)
	// ... (проверка db.Ping() с retry) ...

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
	// Пока NewIndexerKeywordsService не принимает *sql.DB, но в будущем будет
	service := indexerkeywords.NewIndexerKeywordsService(appLogger, rmqClient /*, db */)

	consumeOpts := messaging.ConsumeOpts{
		QueueName:    "tasks.index.keywords.in.queue",
		ExchangeName: messaging.TasksExchange,
		RoutingKey:   messaging.IndexKeywordsTaskRoutingKey,
		ConsumerTag:  "indexer-keywords-consumer-1",
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

	appLogger.Info("Worker-Indexer-Keywords service is now running. Press CTRL+C to exit.")
	sig := <-done
	appLogger.Info("Received signal, shutting down Worker-Indexer-Keywords service...", "signal", sig.String())
	appLogger.Info("Worker-Indexer-Keywords service shut down.")
}
