// File: internal/worker/indexerkeywords/service.go
package indexerkeywords

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage"
	// "database/sql" // Понадобится для PostgreSQL
)

type IndexerKeywordsService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient
	// db *sql.DB // Для PostgreSQL
}

func NewIndexerKeywordsService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient,
	// db *sql.DB, // Для PostgreSQL
) *IndexerKeywordsService {
	return &IndexerKeywordsService{
		logger:      logger.With("component", "indexer_keywords_service"),
		publisher:   publisher,
		minioClient: minioClient,
		// db: db,
	}
}

func (s *IndexerKeywordsService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.IndexKeywordsTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal index keywords task event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal IndexKeywordsTaskEvent: %w", err)
	}

	s.logger.Info("Received index keywords task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"original_file_path", task.OriginalFilePath,
		"processed_data_path", task.ProcessedDataPath)

	result := messaging.IndexKeywordsResultEvent{
		TaskID:            task.TaskID,
		OriginalURL:       task.OriginalURL,
		OriginalFilePath:  task.OriginalFilePath,
		ProcessedDataPath: task.ProcessedDataPath,
		Success:           false,
	}

	// TODO: Реальная логика чтения обработанного текста/markdown из Minio по task.ProcessedDataPath
	// TODO: Реальная логика извлечения ключевых слов и сохранения их в PostgreSQL

	s.logger.Info("Simulating keyword indexing...",
		"task_id", task.TaskID,
		"input_processed_path", task.ProcessedDataPath)
	time.Sleep(1 * time.Second)
	s.logger.Info("Keyword indexing simulation finished", "task_id", task.TaskID)

	result.KeywordsStored = true
	result.Success = true
	result.Message = "Successfully indexed keywords (simulated)"

	return s.publishResultAndAck(result, delivery)
}

func (s *IndexerKeywordsService) publishResultAndAck(result messaging.IndexKeywordsResultEvent, delivery amqp091.Delivery) error {
	pubErr := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsResultRoutingKey, result)
	if pubErr != nil {
		s.logger.Error("Failed to publish index keywords result", "error", pubErr, "task_id", result.TaskID)
	} else {
		s.logger.Info("Index keywords result published",
			"task_id", result.TaskID,
			"success", result.Success,
			"processed_data_path", result.ProcessedDataPath,
			"message", result.Message)
	}

	s.logger.Debug("Attempting to acknowledge original message in IndexerKeywordsService", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original index keywords task message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID, "error", ackErr)
		return fmt.Errorf("failed to Ack index keywords message (tag %d) in IndexerKeywordsService: %w", delivery.DeliveryTag, ackErr)
	}
	s.logger.Info("Successfully acknowledged original index keywords task message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	return nil
}
