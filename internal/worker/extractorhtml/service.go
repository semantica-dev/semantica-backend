// File: internal/worker/extractorhtml/service.go
package extractorhtml

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type ExtractorHTMLService struct {
	logger    *slog.Logger
	publisher *messaging.RabbitMQClient
}

func NewExtractorHTMLService(logger *slog.Logger, publisher *messaging.RabbitMQClient) *ExtractorHTMLService {
	return &ExtractorHTMLService{
		logger:    logger.With("component", "extractor_html_service"),
		publisher: publisher,
	}
}

func (s *ExtractorHTMLService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.ExtractHTMLTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal extract html task event", "error", err, "body", string(delivery.Body))
		return err
	}

	s.logger.Info("Received extract HTML task", "task_id", task.TaskID, "original_url", task.OriginalURL, "raw_data_path", task.RawDataPath)

	s.logger.Info("Simulating HTML extraction...", "task_id", task.TaskID)
	time.Sleep(2 * time.Second)                                               // Имитация работы
	simulatedMarkdownPath := "minio/processed/" + task.TaskID + "/content.md" // Пример
	s.logger.Info("HTML extraction simulation finished", "task_id", task.TaskID, "markdown_path", simulatedMarkdownPath)

	result := messaging.ExtractHTMLResultEvent{
		TaskID:       task.TaskID,
		OriginalURL:  task.OriginalURL,
		MarkdownPath: simulatedMarkdownPath,
		Success:      true,
		Message:      "Successfully extracted HTML (simulated)",
	}

	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish extract HTML result", "error", err, "task_id", task.TaskID)
		return nil // Пока не возвращаем ошибку, чтобы не Nack'ать исходную задачу
	}

	s.logger.Info("Extract HTML result published", "task_id", task.TaskID, "success", result.Success)
	return nil
}
