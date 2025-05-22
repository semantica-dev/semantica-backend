// File: internal/worker/extractorhtml/service.go
package extractorhtml

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

type ExtractorHTMLService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient
}

func NewExtractorHTMLService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient,
) *ExtractorHTMLService {
	return &ExtractorHTMLService{
		logger:      logger.With("component", "extractor_html_service"),
		publisher:   publisher,
		minioClient: minioClient,
	}
}

func (s *ExtractorHTMLService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.ExtractHTMLTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal extract html task event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractHTMLTaskEvent: %w", err)
	}

	s.logger.Info("Received extract HTML task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"raw_data_path", task.RawDataPath)

	result := messaging.ExtractHTMLResultEvent{
		TaskID:      task.TaskID,
		OriginalURL: task.OriginalURL,
		RawDataPath: task.RawDataPath,
		Success:     false,
	}

	s.logger.Info("Simulating HTML extraction...",
		"task_id", task.TaskID,
		"input_raw_path", task.RawDataPath)
	time.Sleep(2 * time.Second)

	simulatedMarkdownPath := fmt.Sprintf("processed/%s/content.md", task.TaskID)
	s.logger.Info("HTML extraction simulation finished",
		"task_id", task.TaskID,
		"generated_markdown_path", simulatedMarkdownPath)

	result.MarkdownPath = simulatedMarkdownPath
	result.Success = true
	result.Message = "Successfully extracted HTML (simulated)"

	return s.publishResultAndAck(result, delivery)
}

func (s *ExtractorHTMLService) publishResultAndAck(result messaging.ExtractHTMLResultEvent, delivery amqp091.Delivery) error {
	pubErr := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLResultRoutingKey, result)
	if pubErr != nil {
		s.logger.Error("Failed to publish extract HTML result", "error", pubErr, "task_id", result.TaskID)
	} else {
		s.logger.Info("Extract HTML result published",
			"task_id", result.TaskID,
			"success", result.Success,
			"raw_data_path", result.RawDataPath,
			"markdown_path", result.MarkdownPath,
			"message", result.Message)
	}

	s.logger.Debug("Attempting to acknowledge original message in ExtractorHTMLService", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original extract HTML task message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID, "error", ackErr)
		return fmt.Errorf("failed to Ack extract HTML message (tag %d) in ExtractorHTMLService: %w", delivery.DeliveryTag, ackErr)
	}
	return nil
}
