// File: internal/orchestrator/publisher/publisher.go
package publisher

import (
	"context"
	"log/slog"

	"github.com/semantica-dev/semantica-backend/pkg/messaging" // Убедитесь, что путь правильный
)

type TaskPublisher struct {
	client *messaging.RabbitMQClient
	logger *slog.Logger
}

func NewTaskPublisher(client *messaging.RabbitMQClient, logger *slog.Logger) *TaskPublisher {
	return &TaskPublisher{
		client: client,
		logger: logger.With("component", "task_publisher"),
	}
}

// PublishCrawlTask публикует задачу на краулинг.
// В будущем здесь могут быть методы для публикации других конкретных типов задач,
// если мы хотим большей типизации и инкапсуляции, чем просто предоставление Client().
func (p *TaskPublisher) PublishCrawlTask(ctx context.Context, task messaging.CrawlTaskEvent) error {
	p.logger.Info("Publishing crawl task", "task_id", task.TaskID, "url", task.URL)
	return p.client.Publish(ctx, messaging.TasksExchange, messaging.CrawlTaskRoutingKey, task)
}

// Client возвращает нижележащий RabbitMQ клиент.
// Это позволяет другим частям Оркестратора (например, TaskListener)
// публиковать различные типы сообщений, используя уже существующее соединение.
// Альтернативой было бы добавление в TaskPublisher отдельных методов для каждого типа сообщения,
// которое Оркестратор может отправлять (PublishExtractHTMLTask, PublishIndexKeywordsTask и т.д.).
// Для MVP предоставление клиента является допустимым упрощением.
func (p *TaskPublisher) Client() *messaging.RabbitMQClient {
	return p.client
}
