// File: internal/worker/indexerkeywords/service.go
package indexerkeywords

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type IndexerKeywordsService struct {
	logger    *slog.Logger
	publisher *messaging.RabbitMQClient
}

func NewIndexerKeywordsService(logger *slog.Logger, publisher *messaging.RabbitMQClient) *IndexerKeywordsService {
	return &IndexerKeywordsService{
		logger:    logger.With("component", "indexer_keywords_service"),
		publisher: publisher,
	}
}

func (s *IndexerKeywordsService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.IndexKeywordsTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal index keywords task event", "error", err, "body", string(delivery.Body))
		return err
	}

	s.logger.Info("Received index keywords task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL, // Логируем для полноты
		"original_file_path", task.OriginalFilePath, // Логируем для полноты
		"processed_data_path", task.ProcessedDataPath)

	s.logger.Info("Simulating keyword indexing...", "task_id", task.TaskID)
	time.Sleep(1 * time.Second) // Имитация работы
	s.logger.Info("Keyword indexing simulation finished", "task_id", task.TaskID)

	result := messaging.IndexKeywordsResultEvent{
		TaskID:            task.TaskID,
		OriginalURL:       task.OriginalURL,
		OriginalFilePath:  task.OriginalFilePath,
		ProcessedDataPath: task.ProcessedDataPath, // <--- ИСПРАВЛЕНИЕ: Передаем ProcessedDataPath дальше
		KeywordsStored:    true,
		Success:           true,
		Message:           "Successfully indexed keywords (simulated)",
	}

	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish index keywords result", "error", err, "task_id", task.TaskID)
		// В реальном приложении здесь может быть логика повторной отправки или обработки ошибки.
		// Для MVP мы Ack'аем исходное сообщение, чтобы не блокировать очередь.
		return nil
	}

	s.logger.Info("Index keywords result published", "task_id", task.TaskID, "success", result.Success)
	return nil
}
