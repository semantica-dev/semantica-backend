// File: internal/worker/extractorother/service.go
package extractorother

import (
	"context"
	"encoding/json"
	"fmt" // Добавим для формирования пути
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
		return err // Nack
	}

	s.logger.Info("Received extract other file task",
		"task_id", task.TaskID,
		"original_file_path", task.OriginalFilePath, // Это имя файла, которое пришло от пользователя или было определено ранее
		"raw_data_path", task.RawDataPath) // Путь к файлу в Minio, который нужно обработать

	// Имитация работы экстрактора для других файлов (PDF, DOCX, TXT)
	s.logger.Info("Simulating other file extraction...",
		"task_id", task.TaskID,
		"input_raw_path", task.RawDataPath)
	time.Sleep(2 * time.Second) // Имитация работы

	// Симулируем генерацию пути к извлеченному текстовому файлу
	simulatedExtractedTextPath := fmt.Sprintf("processed/%s/extracted_text.txt", task.TaskID)

	s.logger.Info("Other file extraction simulation finished",
		"task_id", task.TaskID,
		"generated_text_path", simulatedExtractedTextPath)

	// Формируем результат
	result := messaging.ExtractOtherResultEvent{
		TaskID:            task.TaskID,
		OriginalFilePath:  task.OriginalFilePath,
		RawDataPath:       task.RawDataPath,           // <--- Передаем исходный RawDataPath
		ExtractedTextPath: simulatedExtractedTextPath, // <--- Устанавливаем новый путь к извлеченному тексту
		Success:           true,
		Message:           "Successfully extracted text from other file (simulated)",
	}

	// Публикуем результат
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.ExtractOtherResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish extract other file result", "error", err, "task_id", task.TaskID)
		return nil // Ack исходного сообщения
	}

	s.logger.Info("Extract other file result published",
		"task_id", task.TaskID,
		"success", result.Success,
		"raw_data_path", result.RawDataPath,
		"extracted_text_path", result.ExtractedTextPath)
	return nil // Успешная обработка -> Ack
}
