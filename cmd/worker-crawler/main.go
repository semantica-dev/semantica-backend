// File: cmd/worker-crawler/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/semantica-dev/semantica-backend/internal/worker/crawler"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	cfg := config.LoadConfig()

	appLogger := logger.New("worker-crawler-service", cfg.LogFormat, cfg.GetSlogLevel()) // <--- ИЗМЕНЕНИЕ ЗДЕСЬ
	appLogger.Info("Starting Worker-Crawler service...")
	appLogger.Info("Configuration loaded",
		"rabbitmq_url", cfg.RabbitMQ_URL,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"max_retries", cfg.MaxRetries,
		"retry_interval", cfg.RetryInterval.String(),
	)

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
			case done <- syscall.SIGABRT: // Use a different signal to indicate consumer failure
			default:
			}
		}
	}()

	appLogger.Info("Worker-Crawler service is now running. Press CTRL+C to exit.")
	sig := <-done
	appLogger.Info("Received signal, shutting down Worker-Crawler service...", "signal", sig.String())
	// rmqClient.Close() is already deferred
	appLogger.Info("Worker-Crawler service shut down.")
}
