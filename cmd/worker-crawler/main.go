// File: cmd/worker-crawler/main.go
package main

import (
	"os"
	"os/signal"
	"syscall"
	"time" // Убедимся, что time импортирован для retryInterval

	"github.com/semantica-dev/semantica-backend/internal/worker/crawler"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	appLogger := logger.New("worker-crawler-service")
	appLogger.Info("Starting Worker-Crawler service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL)

	// Параметры для retry подключения к RabbitMQ
	const rabbitMaxRetries = 5
	const rabbitRetryInterval = 5 * time.Second

	rmqClient, err := messaging.NewRabbitMQClient(
		cfg.RabbitMQ_URL,
		appLogger.With("component", "rabbitmq_client"),
		rabbitMaxRetries,
		rabbitRetryInterval,
	)
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client after all retries. Exiting.", "error", err)
		os.Exit(1)
	}
	defer rmqClient.Close() // Воркер проще, можно использовать defer, т.к. Consume блокирующий

	crawlService := crawler.NewCrawlService(appLogger, rmqClient)

	consumeOpts := messaging.ConsumeOpts{
		QueueName:    "tasks.crawl.in.queue",
		ExchangeName: messaging.TasksExchange,
		RoutingKey:   messaging.CrawlTaskRoutingKey,
		ConsumerTag:  "crawler-consumer-1",
	}

	appLogger.Info("Setting up consumer...", "queue", consumeOpts.QueueName, "routing_key", consumeOpts.RoutingKey)

	// Канал для грациозного завершения при получении сигнала ОС
	// Основной цикл работы будет в rmqClient.Consume
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем консьюмер в основной горутине, т.к. он блокирующий
	// и мы хотим, чтобы `main` не завершался, пока он работает или не придет сигнал.
	go func() {
		// Эта горутина нужна, чтобы rmqClient.Consume не блокировал
		// ожидание сигнала в основном потоке main.
		// Если rmqClient.Consume вернет ошибку (например, из-за проблем с соединением после старта),
		// мы хотим, чтобы приложение завершилось.
		if err := rmqClient.Consume(consumeOpts, crawlService.HandleTask); err != nil {
			appLogger.Error("RabbitMQ consumer failed and stopped.", "error", err)
			// Отправляем сигнал, чтобы инициировать завершение main, если еще не получен SIGINT/SIGTERM
			select {
			case done <- syscall.SIGABRT: // Используем SIGABRT для индикации внутренней ошибки
			default: // Если done уже получил сигнал, ничего не делаем
			}
		}
	}()

	appLogger.Info("Worker-Crawler service is now running. Press CTRL+C to exit.")
	sig := <-done // Ждем сигнала SIGINT, SIGTERM или SIGABRT от консьюмера
	appLogger.Info("Received signal, shutting down Worker-Crawler service...", "signal", sig.String())

	// rmqClient.Close() будет вызван через defer при выходе из main.
	// Для воркера это достаточно, так как у него нет HTTP сервера и сложной логики
	// с несколькими консьюмерами в WaitGroup, как у Оркестратора.
	// При закрытии соединения rmqClient.Consume вернется, и горутина завершится.
	appLogger.Info("Worker-Crawler service shut down.")
}
