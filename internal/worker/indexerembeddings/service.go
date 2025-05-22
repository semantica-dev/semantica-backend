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
	"github.com/semantica-dev/semantica-backend/pkg/storage"
	// qdrantClient "github.com/qdrant/go-client/qdrant" // Понадобится для Qdrant
)

type IndexerEmbeddingsService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient
	// qdrantClient *qdrant.PointsClient // Для Qdrant
}

func NewIndexerEmbeddingsService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient,
	// qdrantClient *qdrant.PointsClient, // Для Qdrant
) *IndexerEmbeddingsService {
	return &IndexerEmbeddingsService{
		logger:      logger.With("component", "indexer_embeddings_service"),
		publisher:   publisher,
		minioClient: minioClient,
		// qdrantClient: qdrantClient,
	}
}

func (s *IndexerEmbeddingsService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.IndexEmbeddingsTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal index embeddings task event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal IndexEmbeddingsTaskEvent: %w", err)
	}

	s.logger.Info("Received index embeddings task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"original_file_path", task.OriginalFilePath,
		"processed_data_path", task.ProcessedDataPath)

	result := messaging.IndexEmbeddingsResultEvent{
		TaskID:            task.TaskID,
		OriginalURL:       task.OriginalURL,
		OriginalFilePath:  task.OriginalFilePath,
		ProcessedDataPath: task.ProcessedDataPath,
		Success:           false,
	}

	// TODO: Реальная логика чтения обработанного текста/markdown из Minio по task.ProcessedDataPath
	// TODO: Реальная логика разбиения на чанки, генерации эмбеддингов и сохранения их в Qdrant (и метаданных в PostgreSQL)

	s.logger.Info("Simulating embedding generation and indexing...",
		"task_id", task.TaskID,
		"input_processed_path", task.ProcessedDataPath)
	time.Sleep(3 * time.Second)
	s.logger.Info("Embedding indexing simulation finished", "task_id", task.TaskID)

	result.EmbeddingsStored = true
	result.Success = true
	result.Message = "Successfully indexed embeddings (simulated)"

	// Публикуем результат этапа индексации эмбеддингов
	pubErr := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.IndexEmbeddingsResultRoutingKey, result)
	if pubErr != nil {
		s.logger.Error("Failed to publish index embeddings result", "error", pubErr, "task_id", task.TaskID)
		// Не прерываем, пытаемся отправить TaskProcessingFinishedEvent и Ack'нуть
	} else {
		s.logger.Info("Index embeddings result published",
			"task_id", result.TaskID,
			"success", result.Success,
			"processed_data_path", result.ProcessedDataPath,
			"message", result.Message)
	}

	// Публикуем событие о завершении всей цепочки обработки данных
	finishedEvent := messaging.TaskProcessingFinishedEvent{
		TaskID:           task.TaskID,
		OriginalURL:      task.OriginalURL,
		OriginalFilePath: task.OriginalFilePath,
		OverallSuccess:   result.Success, // Успех этого этапа определяет общий успех (пока что)
		FinalMessage:     fmt.Sprintf("Processing chain finished (simulated). Embeddings stored: %t", result.EmbeddingsStored),
	}
	errFinished := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.TaskProcessingFinishedRoutingKey, finishedEvent)
	if errFinished != nil {
		s.logger.Error("Failed to publish task processing finished event", "error", errFinished, "task_id", task.TaskID)
	} else {
		s.logger.Info("Task processing finished event published", "task_id", task.TaskID, "overall_success", finishedEvent.OverallSuccess)
	}

	// Подтверждаем исходное сообщение IndexEmbeddingsTaskEvent
	s.logger.Debug("Attempting to acknowledge original message in IndexerEmbeddingsService", "delivery_tag", delivery.DeliveryTag, "task_id", task.TaskID)
	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original index embeddings task message", "delivery_tag", delivery.DeliveryTag, "task_id", task.TaskID, "error", ackErr)
		return fmt.Errorf("failed to Ack index embeddings message (tag %d) in IndexerEmbeddingsService: %w", delivery.DeliveryTag, ackErr)
	}
	s.logger.Info("Successfully acknowledged original index embeddings task message", "delivery_tag", delivery.DeliveryTag, "task_id", task.TaskID)
	return nil
}
