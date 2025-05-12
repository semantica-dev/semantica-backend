// File: internal/worker/crawler/service.go
package crawler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging" // Убедитесь, что путь правильный
)

type CrawlService struct {
	logger    *slog.Logger
	publisher *messaging.RabbitMQClient // Воркеру тоже нужен паблишер для отправки результатов
}

func NewCrawlService(logger *slog.Logger, publisher *messaging.RabbitMQClient) *CrawlService {
	return &CrawlService{
		logger:    logger.With("component", "crawl_service"),
		publisher: publisher,
	}
}

// HandleTask обрабатывает полученное сообщение с задачей на краулинг.
// Возвращает ошибку, если сообщение не удалось обработать (будет Nack).
// Возвращает nil при успешной обработке (будет Ack).
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
	s.logger.Info("Crawling simulation finished", "task_id", task.TaskID, "url", task.URL)

	// Условный успех
	result := messaging.CrawlResultEvent{
		TaskID:  task.TaskID,
		URL:     task.URL,
		Success: true,
		Message: "Successfully crawled (simulated)",
	}

	// Публикуем результат
	// Используем context.Background() для MVP
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.CrawlResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish crawl result", "error", err, "task_id", task.TaskID)
		// Если не удалось опубликовать результат, мы все равно считаем, что исходное сообщение
		// обработано (чтобы не зацикливаться на нем). Логика повторной отправки результата -
		// это отдельная задача. Либо можно вернуть ошибку, чтобы исходное сообщение Nack'нулось
		// и потенциально было обработано другим инстансом воркера. Для MVP: считаем обработанным.
		// return err // -> Nack исходного сообщения.
		return nil // -> Ack исходного сообщения, но результат не опубликован. Выберите поведение.
	}

	s.logger.Info("Crawl result published", "task_id", task.TaskID, "success", result.Success)
	return nil // Успешная обработка -> Ack
}
