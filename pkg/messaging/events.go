// File: pkg/messaging/events.go
package messaging

const (
	TasksExchange = "tasks_exchange" // Общий exchange для задач

	CrawlTaskRoutingKey   = "crawl.task.in"    // Роутинг ключ для входящих задач краулинга
	CrawlResultRoutingKey = "crawl.result.out" // Роутинг ключ для исходящих результатов краулинга
)

type CrawlTaskEvent struct {
	TaskID string `json:"task_id"`
	URL    string `json:"url"`
}

type CrawlResultEvent struct {
	TaskID  string `json:"task_id"`
	URL     string `json:"url"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	// В будущем здесь могут быть ссылки на Minio, извлеченный контент и т.д.
}
