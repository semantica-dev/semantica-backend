// File: internal/worker/indexerembeddings/service.go
package indexerembeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type IndexerEmbeddingsService struct {
	logger    *slog.Logger
	publisher *messaging.RabbitMQClient
}

func NewIndexerEmbeddingsService(logger *slog.Logger, publisher *messaging.RabbitMQClient) *IndexerEmbeddingsService {
	return &IndexerEmbeddingsService{
		logger:    logger.With("component", "indexer_embeddings_service"),
		publisher: publisher,
	}
}

func (s *IndexerEmbeddingsService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.IndexEmbeddingsTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal index embeddings task event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	s.logger.Info("Received index embeddings task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"original_file_path", task.OriginalFilePath,
		"processed_data_path", task.ProcessedDataPath) // Логируем полученный ProcessedDataPath

	// Имитация работы индексатора эмбеддингов
	s.logger.Info("Simulating embedding generation and indexing...",
		"task_id", task.TaskID,
		"input_processed_path", task.ProcessedDataPath) // Указываем, какой файл "обрабатываем"
	time.Sleep(3 * time.Second) // Имитация работы (может быть дольше)
	s.logger.Info("Embedding indexing simulation finished", "task_id", task.TaskID)

	// Формируем результат для этого этапа
	result := messaging.IndexEmbeddingsResultEvent{
		TaskID:            task.TaskID,
		OriginalURL:       task.OriginalURL,
		OriginalFilePath:  task.OriginalFilePath,
		ProcessedDataPath: task.ProcessedDataPath, // <--- Передаем ProcessedDataPath дальше
		EmbeddingsStored:  true,                   // Симулируем, что эмбеддинги сохранены
		Success:           true,
		Message:           "Successfully indexed embeddings (simulated)",
	}

	// Публикуем результат этапа индексации эмбеддингов
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.IndexEmbeddingsResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish index embeddings result", "error", err, "task_id", task.TaskID)
		// Продолжаем, чтобы опубликовать TaskProcessingFinishedEvent, но логируем ошибку
	} else {
		s.logger.Info("Index embeddings result published",
			"task_id", task.TaskID,
			"success", result.Success,
			"processed_data_path", result.ProcessedDataPath)
	}

	// Публикуем событие о завершении всей цепочки обработки данных
	finishedEvent := messaging.TaskProcessingFinishedEvent{
		TaskID:           task.TaskID,
		OriginalURL:      task.OriginalURL,
		OriginalFilePath: task.OriginalFilePath,
		OverallSuccess:   result.Success, // Успех этого этапа определяет общий успех (пока что)
		FinalMessage:     fmt.Sprintf("Processing chain finished (simulated). Embeddings stored: %t", result.Success),
	}
	errFinished := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.TaskProcessingFinishedRoutingKey, finishedEvent)
	if errFinished != nil {
		s.logger.Error("Failed to publish task processing finished event", "error", errFinished, "task_id", task.TaskID)
	} else {
		s.logger.Info("Task processing finished event published", "task_id", task.TaskID, "overall_success", finishedEvent.OverallSuccess)
	}

	// Возвращаем nil, чтобы подтвердить (Ack) исходное сообщение IndexEmbeddingsTaskEvent,
	// даже если публикация одного из результатов не удалась (чтобы не блокировать очередь).
	// В реальной системе может потребоваться более сложная логика обработки ошибок публикации.
	return nil
}
