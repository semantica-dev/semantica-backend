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
		return err
	}

	s.logger.Info("Received index embeddings task", "task_id", task.TaskID, "processed_data_path", task.ProcessedDataPath)

	s.logger.Info("Simulating embedding indexing...", "task_id", task.TaskID)
	time.Sleep(3 * time.Second)
	s.logger.Info("Embedding indexing simulation finished", "task_id", task.TaskID)

	result := messaging.IndexEmbeddingsResultEvent{
		TaskID:            task.TaskID,
		OriginalURL:       task.OriginalURL,
		OriginalFilePath:  task.OriginalFilePath,
		ProcessedDataPath: task.ProcessedDataPath,
		EmbeddingsStored:  true,
		Success:           true,
		Message:           "Successfully indexed embeddings (simulated)",
	}

	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.IndexEmbeddingsResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish index embeddings result", "error", err, "task_id", task.TaskID)
	} else {
		s.logger.Info("Index embeddings result published", "task_id", task.TaskID, "success", result.Success)
	}

	finishedEvent := messaging.TaskProcessingFinishedEvent{
		TaskID:           task.TaskID,
		OriginalURL:      task.OriginalURL,
		OriginalFilePath: task.OriginalFilePath,
		OverallSuccess:   result.Success,
		FinalMessage:     fmt.Sprintf("Processing chain finished (simulated). Embeddings stored: %t", result.Success),
	}
	err = s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.TaskProcessingFinishedRoutingKey, finishedEvent)
	if err != nil {
		s.logger.Error("Failed to publish task processing finished event", "error", err, "task_id", task.TaskID)
	} else {
		s.logger.Info("Task processing finished event published", "task_id", task.TaskID)
	}

	return nil
}
