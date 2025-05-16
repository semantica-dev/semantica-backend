// File: cmd/worker-extractor-html/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/semantica-dev/semantica-backend/internal/worker/extractorhtml"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	appLogger := logger.New("worker-extractor-html-service")
	appLogger.Info("Starting Worker-Extractor-HTML service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL)

	rmqClient, err := messaging.NewRabbitMQClient(cfg.RabbitMQ_URL, appLogger.With("component", "rabbitmq_client"))
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client. Exiting.", "error", err)
		os.Exit(1)
	}
	defer rmqClient.Close()

	service := extractorhtml.NewExtractorHTMLService(appLogger, rmqClient)

	consumeOpts := messaging.ConsumeOpts{
		QueueName:    "tasks.extract.html.in.queue",
		ExchangeName: messaging.TasksExchange,
		RoutingKey:   messaging.ExtractHTMLTaskRoutingKey,
		ConsumerTag:  "extractor-html-consumer-1",
	}

	appLogger.Info("Setting up consumer...", "queue", consumeOpts.QueueName, "routing_key", consumeOpts.RoutingKey)
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		err := rmqClient.Consume(consumeOpts, service.HandleTask)
		if err != nil {
			appLogger.Error("RabbitMQ consumer failed", "error", err)
			done <- syscall.SIGABRT
		}
	}()

	appLogger.Info("Worker-Extractor-HTML service is now running. Press CTRL+C to exit.")
	<-done
	appLogger.Info("Worker-Extractor-HTML service shutting down...")
}
