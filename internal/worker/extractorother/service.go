// File: internal/worker/extractorother/service.go
package extractorother

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage"
)

type ExtractorOtherService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient
}

func NewExtractorOtherService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient,
) *ExtractorOtherService {
	return &ExtractorOtherService{
		logger:      logger.With("component", "extractor_other_service"),
		publisher:   publisher,
		minioClient: minioClient,
	}
}

func (s *ExtractorOtherService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.ExtractOtherTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal extract other task event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractOtherTaskEvent: %w", err)
	}

	s.logger.Info("Received extract other file task",
		"task_id", task.TaskID,
		"original_file_path", task.OriginalFilePath,
		"raw_data_path", task.RawDataPath)

	result := messaging.ExtractOtherResultEvent{
		TaskID:           task.TaskID,
		OriginalFilePath: task.OriginalFilePath,
		RawDataPath:      task.RawDataPath,
		Success:          false,
	}

	// TODO: Реальная логика чтения из Minio по task.RawDataPath
	// TODO: Реальная логика извлечения текста из PDF, DOCX, TXT

	s.logger.Info("Simulating other file extraction...",
		"task_id", task.TaskID,
		"input_raw_path", task.RawDataPath)
	time.Sleep(2 * time.Second)

	simulatedExtractedTextPath := fmt.Sprintf("processed/%s/extracted_text.txt", task.TaskID)
	s.logger.Info("Other file extraction simulation finished",
		"task_id", task.TaskID,
		"generated_text_path", simulatedExtractedTextPath)

	// TODO: Реальная загрузка извлеченного текста в Minio

	result.ExtractedTextPath = simulatedExtractedTextPath
	result.Success = true
	result.Message = "Successfully extracted text from other file (simulated)"

	return s.publishResultAndAck(result, delivery)
}

func (s *ExtractorOtherService) publishResultAndAck(result messaging.ExtractOtherResultEvent, delivery amqp091.Delivery) error {
	pubErr := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractOtherResultRoutingKey, result)
	if pubErr != nil {
		s.logger.Error("Failed to publish extract other file result", "error", pubErr, "task_id", result.TaskID)
	} else {
		s.logger.Info("Extract other file result published",
			"task_id", result.TaskID,
			"success", result.Success,
			"raw_data_path", result.RawDataPath,
			"extracted_text_path", result.ExtractedTextPath,
			"message", result.Message)
	}

	s.logger.Debug("Attempting to acknowledge original message in ExtractorOtherService", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original extract other task message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID, "error", ackErr)
		return fmt.Errorf("failed to Ack extract other message (tag %d) in ExtractorOtherService: %w", delivery.DeliveryTag, ackErr)
	}
	s.logger.Info("Successfully acknowledged original extract other task message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	return nil
}
