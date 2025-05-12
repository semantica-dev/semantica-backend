// File: cmd/orchestrator/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/semantica-dev/semantica-backend/internal/orchestrator/api" // Убедитесь, что пути правильные
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/config"
	"github.com/semantica-dev/semantica-backend/pkg/logger"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

func main() {
	appLogger := logger.New("orchestrator-service")
	appLogger.Info("Starting Orchestrator service...")

	cfg := config.LoadConfig()
	appLogger.Info("Configuration loaded", "rabbitmq_url", cfg.RabbitMQ_URL, "api_port", cfg.Orchestrator_API_Port)

	// Инициализация RabbitMQ клиента
	rmqClient, err := messaging.NewRabbitMQClient(cfg.RabbitMQ_URL, appLogger.With("component", "rabbitmq_client"))
	if err != nil {
		appLogger.Error("Failed to initialize RabbitMQ client. Exiting.", "error", err)
		os.Exit(1)
	}
	defer rmqClient.Close()

	taskPublisher := publisher.NewTaskPublisher(rmqClient, appLogger)
	taskAPIHandler := api.NewTaskAPIHandler(appLogger, taskPublisher)

	// Настройка HTTP сервера
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/crawl", taskAPIHandler.CreateCrawlTaskHandler)
	// Можно добавить /health эндпоинт
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.Orchestrator_API_Port,
		Handler: mux,
		// ReadTimeout:  5 * time.Second, // Можно добавить таймауты
		// WriteTimeout: 10 * time.Second,
		// IdleTimeout:  120 * time.Second,
	}

	// Горутина для грациозного завершения
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		appLogger.Info("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			appLogger.Error("Server forced to shutdown:", "error", err)
		}
	}()

	appLogger.Info("Orchestrator API server starting", "port", cfg.Orchestrator_API_Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		appLogger.Error("Could not listen on", "port", cfg.Orchestrator_API_Port, "error", err)
		os.Exit(1)
	}

	appLogger.Info("Server stopped")
}
