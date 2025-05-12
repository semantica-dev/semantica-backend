// File: cmd/worker-crawler/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/semantica-dev/semantica-backend/internal/worker/crawler" // Убедитесь, что пути правильные
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	appLogger := logger.New("worker-crawler-service")
	appLogger.Info("Starting Worker-Crawler service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL)

	// Инициализация RabbitMQ клиента. Воркеру он нужен и для чтения, и для записи результатов.
	rmqClient, err := messaging.NewRabbitMQClient(cfg.RabbitMQ_URL, appLogger.With("component", "rabbitmq_client"))
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client. Exiting.", "error", err)
		os.Exit(1)
	}
	defer rmqClient.Close()

	crawlService := crawler.NewCrawlService(appLogger, rmqClient) // Передаем rmqClient как паблишер

	consumeOpts := messaging.ConsumeOpts{
		QueueName:    "tasks.crawl.in.queue", // Дадим очереди уникальное имя
		ExchangeName: messaging.TasksExchange,
		RoutingKey:   messaging.CrawlTaskRoutingKey, // Слушаем задачи на краулинг
		ConsumerTag:  "crawler-consumer-1",          // Уникальный тег для этого консьюмера
	}

	appLogger.Info("Setting up consumer...", "queue", consumeOpts.QueueName, "routing_key", consumeOpts.RoutingKey)

	// Канал для грациозного завершения
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем консьюмер в отдельной горутине, чтобы он не блокировал основной поток
	// и мы могли дождаться сигнала о завершении.
	go func() {
		err := rmqClient.Consume(consumeOpts, crawlService.HandleTask)
		if err != nil {
			appLogger.Error("RabbitMQ consumer failed", "error", err)
			// Если консьюмер упал, вероятно, стоит завершить приложение
			// или попытаться его перезапустить. Для MVP просто логируем и позволяем done сработать.
			done <- syscall.SIGABRT // Сигнализируем о проблеме
		}
	}()

	appLogger.Info("Worker-Crawler service is now running. Press CTRL+C to exit.")
	<-done // Ждем сигнала SIGINT или SIGTERM
	appLogger.Info("Worker-Crawler service shutting down...")
	// rmqClient.Close() будет вызван через defer
}
