// File: internal/worker/extractorother/service.go
package extractorother

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type ExtractorOtherService struct {
	logger    *slog.Logger
	publisher *messaging.RabbitMQClient
}

func NewExtractorOtherService(logger *slog.Logger, publisher *messaging.RabbitMQClient) *ExtractorOtherService {
	return &ExtractorOtherService{
		logger:    logger.With("component", "extractor_other_service"),
		publisher: publisher,
	}
}

func (s *ExtractorOtherService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.ExtractOtherTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal extract other task event", "error", err, "body", string(delivery.Body))
		return err
	}

	s.logger.Info("Received extract other task", "task_id", task.TaskID, "original_file_path", task.OriginalFilePath, "raw_data_path", task.RawDataPath)

	s.logger.Info("Simulating other file extraction...", "task_id", task.TaskID)
	time.Sleep(2 * time.Second)                                                   // Имитация работы
	simulatedTextPath := "minio/processed/" + task.TaskID + "/extracted_text.txt" // Пример
	s.logger.Info("Other file extraction simulation finished", "task_id", task.TaskID, "extracted_text_path", simulatedTextPath)

	result := messaging.ExtractOtherResultEvent{
		TaskID:            task.TaskID,
		OriginalFilePath:  task.OriginalFilePath,
		ExtractedTextPath: simulatedTextPath,
		Success:           true,
		Message:           "Successfully extracted other file (simulated)",
	}

	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractOtherResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish extract other result", "error", err, "task_id", task.TaskID)
		return nil
	}

	s.logger.Info("Extract other result published", "task_id", task.TaskID, "success", result.Success)
	return nil
}
