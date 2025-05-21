// File: internal/worker/extractorhtml/service.go
package extractorhtml

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
	// "mime" // Пока не используется здесь, но может понадобиться для чтения из Minio

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage" // Убедимся, что импорт есть
)

type ExtractorHTMLService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient // <--- ДОБАВЛЕНО ПОЛЕ
}

func NewExtractorHTMLService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient, // <--- ДОБАВЛЕН АРГУМЕНТ
) *ExtractorHTMLService {
	return &ExtractorHTMLService{
		logger:      logger.With("component", "extractor_html_service"),
		publisher:   publisher,
		minioClient: minioClient, // <--- ПРИСВОЕНО ПОЛЕ
	}
}

func (s *ExtractorHTMLService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.ExtractHTMLTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal extract html task event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractHTMLTaskEvent: %w", err) // Возвращаем ошибку для Nack/перезапуска
	}

	s.logger.Info("Received extract HTML task",
		"task_id", task.TaskID,
		"original_url", task.OriginalURL,
		"raw_data_path", task.RawDataPath)

	// Создаем результат по умолчанию (неуспешный)
	result := messaging.ExtractHTMLResultEvent{
		TaskID:      task.TaskID,
		OriginalURL: task.OriginalURL,
		RawDataPath: task.RawDataPath, // Важно передать исходный путь
		Success:     false,
	}

	// TODO: Реальная логика чтения из Minio по task.RawDataPath
	// s.minioClient.GetObject(ctx, task.RawDataPath)
	// TODO: Реальная логика извлечения HTML и конвертации в Markdown
	// Например, с использованием go-readability и html-to-markdown

	// Имитация работы экстрактора HTML
	s.logger.Info("Simulating HTML extraction...",
		"task_id", task.TaskID,
		"input_raw_path", task.RawDataPath)
	time.Sleep(2 * time.Second) // Имитация работы

	// Симулируем генерацию пути к обработанному Markdown файлу
	// В реальном приложении имя файла будет генерироваться, а затем файл загружаться в Minio
	simulatedMarkdownPath := fmt.Sprintf("processed/%s/content.md", task.TaskID)

	s.logger.Info("HTML extraction simulation finished",
		"task_id", task.TaskID,
		"generated_markdown_path", simulatedMarkdownPath)

	// TODO: Реальная загрузка обработанного Markdown/текста в Minio
	// s.minioClient.UploadObject(ctx, simulatedMarkdownPath, ...)

	result.MarkdownPath = simulatedMarkdownPath
	result.Success = true
	result.Message = "Successfully extracted HTML (simulated)"

	return s.publishResultAndAck(result, delivery)
}

// publishResultAndAck инкапсулирует логику публикации результата и подтверждения исходного сообщения.
func (s *ExtractorHTMLService) publishResultAndAck(result messaging.ExtractHTMLResultEvent, delivery amqp091.Delivery) error {
	pubErr := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLResultRoutingKey, result)
	if pubErr != nil {
		s.logger.Error("Failed to publish extract HTML result", "error", pubErr, "task_id", result.TaskID)
		// Логируем, но все равно пытаемся Ack'нуть исходное сообщение
	} else {
		s.logger.Info("Extract HTML result published",
			"task_id", result.TaskID,
			"success", result.Success,
			"raw_data_path", result.RawDataPath,
			"markdown_path", result.MarkdownPath,
			"message", result.Message)
	}

	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original extract HTML task message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID, "error", ackErr)
		return fmt.Errorf("failed to Ack extract HTML message (tag %d): %w", delivery.DeliveryTag, ackErr)
	}
	s.logger.Debug("Original extract HTML task message acknowledged successfully", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	return nil
}
