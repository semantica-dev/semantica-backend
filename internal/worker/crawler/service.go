// File: internal/worker/crawler/service.go
package crawler

import (
	"bytes" // <--- ДОБАВЛЕНО для io.Reader из []byte
	"context"
	"encoding/json"
	"fmt"
	"io" // <--- ДОБАВЛЕНО для io.ReadAll
	"log/slog"
	"mime"
	"net/http" // <--- ДОБАВЛЕНО для HTTP запросов
	"time"

	"github.com/google/uuid" // <--- ДОБАВЛЕНО для уникальных имен файлов
	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage" // <--- ДОБАВЛЕНО
)

type CrawlService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient // <--- ДОБАВЛЕНО
}

func NewCrawlService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient, // <--- ДОБАВЛЕНО
) *CrawlService {
	return &CrawlService{
		logger:      logger.With("component", "crawl_service"),
		publisher:   publisher,
		minioClient: minioClient, // <--- ДОБАВЛЕНО
	}
}

// HandleTask обрабатывает полученное сообщение с задачей на краулинг.
func (s *CrawlService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.CrawlTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal crawl task event", "error", err, "body", string(delivery.Body))
		return err // Ошибка -> Nack
	}

	s.logger.Info("Received crawl task", "task_id", task.TaskID, "url", task.URL)

	// Создаем результат по умолчанию (неуспешный)
	result := messaging.CrawlResultEvent{
		TaskID:  task.TaskID,
		URL:     task.URL,
		Success: false,
	}

	// --- 1. Скачивание HTML-страницы ---
	s.logger.Info("Starting to crawl URL", "task_id", task.TaskID, "url", task.URL)

	// Устанавливаем таймаут для HTTP запроса
	httpClient := http.Client{
		Timeout: 30 * time.Second, // Таймаут для всего запроса, включая чтение тела
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "GET", task.URL, nil)
	if err != nil {
		s.logger.Error("Failed to create HTTP request", "task_id", task.TaskID, "url", task.URL, "error", err)
		result.Message = fmt.Sprintf("Failed to create HTTP request: %v", err)
		// Публикуем результат (неуспешный) и выходим
		return s.publishResultAndAck(result, delivery)
	}
	// Устанавливаем простой User-Agent, чтобы не выглядеть совсем как бот
	httpReq.Header.Set("User-Agent", "SemanticaCrawler/1.0 (+https://semantica.dev/bot)")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		s.logger.Error("Failed to fetch URL", "task_id", task.TaskID, "url", task.URL, "error", err)
		result.Message = fmt.Sprintf("Failed to fetch URL: %v", err)
		return s.publishResultAndAck(result, delivery)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("Received non-OK HTTP status for URL", "task_id", task.TaskID, "url", task.URL, "status_code", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024)) // Читаем немного тела для диагностики
		result.Message = fmt.Sprintf("Received non-OK HTTP status: %d. Body preview: %s", resp.StatusCode, string(bodyBytes))
		return s.publishResultAndAck(result, delivery)
	}

	// Читаем тело ответа
	htmlBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("Failed to read response body", "task_id", task.TaskID, "url", task.URL, "error", err)
		result.Message = fmt.Sprintf("Failed to read response body: %v", err)
		return s.publishResultAndAck(result, delivery)
	}
	s.logger.Info("Successfully fetched URL content", "task_id", task.TaskID, "url", task.URL, "content_length", len(htmlBody))

	// --- 2. Сохранение в Minio ---
	// Генерируем уникальное имя файла и путь для Minio
	fileName := uuid.NewString() + ".html"
	objectName := fmt.Sprintf("raw/%s/%s", task.TaskID, fileName) // Пример: raw/task-uuid-123/file-uuid-abc.html

	s.logger.Info("Preparing to upload raw data to Minio", "task_id", task.TaskID, "object_name", objectName)

	// Создаем reader из скачанных байтов
	reader := bytes.NewReader(htmlBody)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream" // Дефолт, если не определен
	}
	// Часто для HTML это будет text/html; charset=... Упростим до text/html для Minio если оно там есть
	if mainContentType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = mainContentType
	}

	uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 1*time.Minute) // Таймаут на загрузку
	defer uploadCancel()

	uploadInfo, err := s.minioClient.UploadObject(uploadCtx, objectName, reader, int64(len(htmlBody)), contentType)
	if err != nil {
		s.logger.Error("Failed to upload raw data to Minio", "task_id", task.TaskID, "object_name", objectName, "error", err)
		result.Message = fmt.Sprintf("Failed to upload raw data to Minio: %v", err)
		return s.publishResultAndAck(result, delivery)
	}

	s.logger.Info("Raw data uploaded to Minio successfully",
		"task_id", task.TaskID,
		"object_name", objectName,
		"etag", uploadInfo.ETag,
		"size", uploadInfo.Size)

	// --- 3. Формируем успешный результат ---
	result.Success = true
	result.Message = "Successfully crawled and stored raw data"
	result.RawDataPath = objectName // <--- ПУТЬ К РЕАЛЬНОМУ ОБЪЕКТУ В MINIO

	return s.publishResultAndAck(result, delivery)
}

// publishResultAndAck инкапсулирует логику публикации результата и подтверждения исходного сообщения.
func (s *CrawlService) publishResultAndAck(result messaging.CrawlResultEvent, delivery amqp091.Delivery) error {
	// Публикуем результат
	err := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.CrawlResultRoutingKey, result)
	if err != nil {
		s.logger.Error("Failed to publish crawl result", "error", err, "task_id", result.TaskID, "success", result.Success)
		// Если не удалось опубликовать результат, мы все равно считаем, что исходное сообщение
		// обработано (чтобы не зацикливаться на нем). Логика повторной отправки результата -
		// это отдельная задача. Для MVP: считаем обработанным.
		// Ошибку не возвращаем, чтобы исходное сообщение было Ack'нуто.
	} else {
		s.logger.Info("Crawl result published", "task_id", result.TaskID, "success", result.Success, "raw_data_path", result.RawDataPath, "message", result.Message)
	}

	// Подтверждаем исходное сообщение (Ack)
	// Ошибку от Ack можно проигнорировать для упрощения, или залогировать как Warn/Error.
	// Если Ack не удался, сообщение может быть повторно доставлено, что приведет к дублированию
	// обработки. Это отдельный вопрос для улучшения надежности.
	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID, "error", ackErr)
		return ackErr // Если Ack не удался, это серьезная проблема, возвращаем ошибку, чтобы RabbitMQ знал.
	}
	return nil // Успешная обработка (или неуспешная, но результат опубликован)
}
