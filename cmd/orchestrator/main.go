// File: cmd/orchestrator/main.go
package main

import (
	"context"
	// "log/slog" // Используем slog, а не стандартный log
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	// Драйвер PostgreSQL должен быть импортирован для регистрации в database/sql.
	// Это необходимо, так как pkg/database будет использовать sql.Open("postgres", ...).
	_ "github.com/lib/pq"

	"github.com/semantica-dev/semantica-backend/internal/orchestrator/api"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/listener"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/database" // Наш пакет для работы с БД, включая миграции
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"

	"github.com/rabbitmq/amqp091-go" // Явный импорт, если нужен тип в startConsumer
)

func main() {
	appLogger := logger.New("orchestrator-service")
	appLogger.Info("Starting Orchestrator service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL, "api_port", cfg.Orchestrator_API_Port)

	// --- Применение миграций базы данных ---
	postgresDSN := os.Getenv("POSTGRES_DSN")
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "db/migrations" // Значение по умолчанию для локального запуска
		appLogger.Info("MIGRATIONS_DIR not set, using default", "path", migrationsDir)
	}

	if postgresDSN != "" { // Применяем миграции только если DSN задан
		if err := database.ApplyMigrations(appLogger, "postgres", postgresDSN, migrationsDir); err != nil {
			appLogger.Error("Database migration failed. Exiting.", "error", err)
			os.Exit(1) // Критическая ошибка, если миграции не прошли
		}
	} else {
		// В реальном приложении, если Оркестратору критически нужна БД, здесь тоже должен быть os.Exit(1)
		appLogger.Warn("POSTGRES_DSN is not set. Orchestrator might not function correctly without a database for storing task states.")
	}
	// --- Миграции применены (или пропущены, если DSN не был задан) ---

	rmqClient, err := messaging.NewRabbitMQClient(cfg.RabbitMQ_URL, appLogger.With("component", "rabbitmq_client"))
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client. Exiting.", "error", err)
		os.Exit(1)
	}
	// defer rmqClient.Close() // Закрываем явно в конце main для корректной работы с wg.Wait()

	// Инициализация компонентов Оркестратора
	taskPublisher := publisher.NewTaskPublisher(rmqClient, appLogger)
	taskAPIHandler := api.NewTaskAPIHandler(appLogger, taskPublisher)  // Предполагаем, что APIHandler не требует *sql.DB на данном этапе
	taskListener := listener.NewTaskListener(appLogger, taskPublisher) // Предполагаем, что Listener не требует *sql.DB на данном этапе

	// Настройка HTTP сервера
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/crawl", taskAPIHandler.CreateCrawlTaskHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.Orchestrator_API_Port,
		Handler: mux,
		// Можно добавить ReadTimeout, WriteTimeout, IdleTimeout для большей надежности
	}

	// WaitGroup для ожидания завершения всех горутин консьюмеров
	var wg sync.WaitGroup

	// Вспомогательная функция для запуска консьюмеров
	startConsumer := func(consumerOpts messaging.ConsumeOpts, handlerFunc func(delivery amqp091.Delivery) error) {
		defer wg.Done() // Сообщаем WaitGroup о завершении этой горутины
		appLogger.Info("Orchestrator starting consumer",
			"queue", consumerOpts.QueueName,
			"routing_key", consumerOpts.RoutingKey,
			"consumer_tag", consumerOpts.ConsumerTag)

		// Блокирующий вызов Consume. Завершится, когда соединение RabbitMQ будет закрыто или произойдет ошибка.
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

	// Запуск всех консьюмеров Оркестратора
	consumers := []struct {
		opts    messaging.ConsumeOpts
		handler func(delivery amqp091.Delivery) error
	}{
		{messaging.ConsumeOpts{QueueName: "orchestrator.crawl.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.CrawlResultRoutingKey, ConsumerTag: "orchestrator-crawl-result-consumer"}, taskListener.HandleCrawlResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.extract_html.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractHTMLResultRoutingKey, ConsumerTag: "orchestrator-extract-html-result-consumer"}, taskListener.HandleExtractHTMLResult},
		// {messaging.ConsumeOpts{QueueName: "orchestrator.extract_other.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.ExtractOtherResultRoutingKey, ConsumerTag: "orchestrator-extract-other-result-consumer"}, taskListener.HandleExtractOtherResult}, // Закомментирован, т.к. пока не используется активно
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_keywords.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexKeywordsResultRoutingKey, ConsumerTag: "orchestrator-index-keywords-result-consumer"}, taskListener.HandleIndexKeywordsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.index_embeddings.results.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.IndexEmbeddingsResultRoutingKey, ConsumerTag: "orchestrator-index-embeddings-result-consumer"}, taskListener.HandleIndexEmbeddingsResult},
		{messaging.ConsumeOpts{QueueName: "orchestrator.task_finished.queue", ExchangeName: messaging.TasksExchange, RoutingKey: messaging.TaskProcessingFinishedRoutingKey, ConsumerTag: "orchestrator-task-finished-consumer"}, taskListener.HandleTaskProcessingFinished},
	}

	for _, c := range consumers {
		wg.Add(1) // Увеличиваем счетчик WaitGroup для каждой запускаемой горутины
		go startConsumer(c.opts, c.handler)
	}

	// Канал для обработки сигналов ОС для грациозного завершения
	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	// Канал для сигнализации о завершении HTTP сервера
	httpServerDone := make(chan struct{})

	// Горутина для запуска и остановки HTTP сервера
	go func() {
		appLogger.Info("Orchestrator API server starting", "port", cfg.Orchestrator_API_Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Could not listen on HTTP port", "port", cfg.Orchestrator_API_Port, "error", err)
			// Если HTTP сервер не может запуститься, это критично. Инициируем остановку всего приложения.
			// Отправляем сигнал самим себе, чтобы запустить общую логику завершения.
			shutdownSignal <- syscall.SIGTERM
		}
		// Если ListenAndServe завершился без ошибки (например, через server.Shutdown()),
		// или с ошибкой http.ErrServerClosed, мы здесь.
		close(httpServerDone) // Сигнализируем, что HTTP сервер завершил работу
	}()

	// Ожидаем сигнала на завершение (SIGINT/SIGTERM) или пока HTTP сервер не упадет
	<-shutdownSignal
	appLogger.Info("Shutdown signal received. Initiating graceful shutdown...")

	// Инициируем остановку HTTP сервера (если он еще работает)
	// Даем ему время на завершение обработки текущих запросов.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	appLogger.Info("Attempting to shut down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Error("HTTP Server shutdown error", "error", err)
	}

	// Ждем фактического завершения HTTP сервера (если он еще не завершился из-за ошибки ListenAndServe)
	<-httpServerDone
	appLogger.Info("HTTP Server stopped.")

	// Теперь, когда HTTP сервер остановлен, закрываем соединение с RabbitMQ.
	// Это приведет к тому, что блокирующие вызовы rmqClient.Consume() в горутинах консьюмеров завершатся.
	appLogger.Info("Closing RabbitMQ connection to stop consumers...")
	rmqClient.Close()

	// Ожидаем завершения всех горутин консьюмеров
	appLogger.Info("Waiting for all RabbitMQ consumers to finish...")
	wg.Wait()

	appLogger.Info("Orchestrator service shut down gracefully.")
}
