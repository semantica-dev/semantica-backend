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
		return err // Nack
	}

	s.logger.Info("Received index keywords task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"original_file_path", task.OriginalFilePath,
		"processed_data_path", task.ProcessedDataPath) // Логируем полученный ProcessedDataPath

	// Имитация работы индексатора ключевых слов
	s.logger.Info("Simulating keyword indexing...",
		"task_id", task.TaskID,
		"input_processed_path", task.ProcessedDataPath) // Указываем, какой файл "обрабатываем"
	time.Sleep(1 * time.Second) // Имитация работы
	s.logger.Info("Keyword indexing simulation finished", "task_id", task.TaskID)

	// Формируем результат
	result := messaging.IndexKeywordsResultEvent{
		TaskID:            task.TaskID,
		OriginalURL:       task.OriginalURL,
		OriginalFilePath:  task.OriginalFilePath,
		ProcessedDataPath: task.ProcessedDataPath, // <--- Передаем ProcessedDataPath дальше
		KeywordsStored:    true,                   // Симулируем, что ключевые слова сохранены
		Success:           true,
		Message:           "Successfully indexed keywords (simulated)",
	}

	// Публикуем результат
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish index keywords result", "error", err, "task_id", task.TaskID)
		return nil // Ack исходного сообщения
	}

	s.logger.Info("Index keywords result published",
		"task_id", task.TaskID,
		"success", result.Success,
		"processed_data_path", result.ProcessedDataPath)
	return nil // Успешная обработка -> Ack
}
