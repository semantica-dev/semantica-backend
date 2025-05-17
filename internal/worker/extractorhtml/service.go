// File: internal/worker/extractorhtml/service.go
package extractorhtml

import (
	"context"
	"encoding/json"
	"fmt" // Добавим для формирования пути
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
		return err // Nack
	}

	s.logger.Info("Received extract HTML task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"raw_data_path", task.RawDataPath) // Логируем полученный RawDataPath

	// Имитация работы экстрактора HTML
	s.logger.Info("Simulating HTML extraction...",
		"task_id", task.TaskID,
		"input_raw_path", task.RawDataPath) // Указываем, какой файл "обрабатываем"
	time.Sleep(2 * time.Second) // Имитация работы

	// Симулируем генерацию пути к обработанному Markdown файлу
	simulatedMarkdownPath := fmt.Sprintf("processed/%s/content.md", task.TaskID)

	s.logger.Info("HTML extraction simulation finished",
		"task_id", task.TaskID,
		"generated_markdown_path", simulatedMarkdownPath)

	// Формируем результат
	result := messaging.ExtractHTMLResultEvent{
		TaskID:       task.TaskID,
		OriginalURL:  task.OriginalURL,
		RawDataPath:  task.RawDataPath,      // <--- Передаем исходный RawDataPath
		MarkdownPath: simulatedMarkdownPath, // <--- Устанавливаем новый путь к Markdown
		Success:      true,
		Message:      "Successfully extracted HTML (simulated)",
	}

	// Публикуем результат
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish extract HTML result", "error", err, "task_id", task.TaskID)
		return nil // Ack исходного сообщения, чтобы не блокировать очередь.
	}

	s.logger.Info("Extract HTML result published",
		"task_id", task.TaskID,
		"success", result.Success,
		"raw_data_path", result.RawDataPath,
		"markdown_path", result.MarkdownPath)
	return nil // Успешная обработка -> Ack
}
