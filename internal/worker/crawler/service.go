// File: internal/worker/crawler/service.go
package crawler

import (
	"context"
	"encoding/json"
	"fmt" // Добавим для формирования пути
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type CrawlService struct {
	logger    *slog.Logger
	publisher *messaging.RabbitMQClient
}

func NewCrawlService(logger *slog.Logger, publisher *messaging.RabbitMQClient) *CrawlService {
	return &CrawlService{
		logger:    logger.With("component", "crawl_service"),
		publisher: publisher,
	}
}

// HandleTask обрабатывает полученное сообщение с задачей на краулинг.
func (s *CrawlService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.CrawlTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal crawl task event", "error", err, "body", string(delivery.Body))
		return err // Ошибка -> Nack (requeue=false в нашем RabbitMQ клиенте)
	}

	s.logger.Info("Received crawl task", "task_id", task.TaskID, "url", task.URL)

	// Имитация работы краулера
	s.logger.Info("Simulating crawling...", "task_id", task.TaskID, "url", task.URL)
	time.Sleep(2 * time.Second) // Имитируем задержку

	// Симулируем генерацию пути к сырым данным в Minio
	// В реальном приложении здесь будет имя файла, возможно, основанное на URL или сгенерированное
	// и оно будет загружено в Minio, а этот путь будет результатом операции загрузки.
	// Для уникальности используем TaskID. Расширение .sim для симулированных данных.
	simulatedRawDataPath := fmt.Sprintf("raw/%s/page_content.sim", task.TaskID)

	s.logger.Info("Crawling simulation finished", "task_id", task.TaskID, "url", task.URL, "simulated_raw_path", simulatedRawDataPath)

	// Формируем результат
	result := messaging.CrawlResultEvent{
		TaskID:      task.TaskID,
		URL:         task.URL,
		Success:     true,
		Message:     "Successfully crawled (simulated)",
		RawDataPath: simulatedRawDataPath, // <--- ЗАПОЛНЯЕМ СИМУЛИРОВАННЫМ ПУТЕМ
	}

	// Публикуем результат
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.CrawlResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish crawl result", "error", err, "task_id", task.TaskID)
		// Если не удалось опубликовать результат, мы все равно считаем, что исходное сообщение
		// обработано (чтобы не зацикливаться на нем). Логика повторной отправки результата -
		// это отдельная задача. Для MVP: считаем обработанным.
		return nil // -> Ack исходного сообщения, но результат не опубликован.
	}

	s.logger.Info("Crawl result published", "task_id", task.TaskID, "success", result.Success, "raw_data_path", result.RawDataPath)
	return nil // Успешная обработка -> Ack
}
