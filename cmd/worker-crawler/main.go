// File: cmd/worker-crawler/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"
	// time импортируется неявно через config, но лучше явно, если используется напрямую
	// "time"

	"github.com/semantica-dev/semantica-backend/internal/worker/crawler"
	"github.com/semantica-dev/semantica-backend/pkg/config" // Используем наш пакет config
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	cfg := config.LoadConfig() // 1. Загружаем конфигурацию

	appLogger := logger.New("worker-crawler-service") // 2. Инициализируем логгер
	appLogger.Info("Starting Worker-Crawler service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		// Можно добавить другие релевантные поля из cfg для логирования, если нужно
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

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
	crawlService := crawler.NewCrawlService(appLogger, rmqClient)

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
