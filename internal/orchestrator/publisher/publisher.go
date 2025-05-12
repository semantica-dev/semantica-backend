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

func (p *TaskPublisher) PublishCrawlTask(ctx context.Context, task messaging.CrawlTaskEvent) error {
	p.logger.Info("Publishing crawl task", "task_id", task.TaskID, "url", task.URL)
	return p.client.Publish(ctx, messaging.TasksExchange, messaging.CrawlTaskRoutingKey, task)
}
