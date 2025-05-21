// File: internal/worker/crawler/service.go
package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime" // <--- Убедись, что этот импорт есть
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
	"github.com/semantica-dev/semantica-backend/pkg/storage"
)

type CrawlService struct {
	logger      *slog.Logger
	publisher   *messaging.RabbitMQClient
	minioClient *storage.MinioClient
}

func NewCrawlService(
	logger *slog.Logger,
	publisher *messaging.RabbitMQClient,
	minioClient *storage.MinioClient,
) *CrawlService {
	return &CrawlService{
		logger:      logger.With("component", "crawl_service"),
		publisher:   publisher,
		minioClient: minioClient,
	}
}

func (s *CrawlService) HandleTask(delivery amqp091.Delivery) error {
	var task messaging.CrawlTaskEvent
	if err := json.Unmarshal(delivery.Body, &task); err != nil {
		s.logger.Error("Failed to unmarshal crawl task event", "error", err, "body", string(delivery.Body))
		return err
	}

	s.logger.Info("Received crawl task", "task_id", task.TaskID, "url", task.URL)

	result := messaging.CrawlResultEvent{
		TaskID:  task.TaskID,
		URL:     task.URL,
		Success: false,
	}

	s.logger.Info("Starting to crawl URL", "task_id", task.TaskID, "url", task.URL)
	httpClient := http.Client{Timeout: 30 * time.Second}
	httpReq, err := http.NewRequestWithContext(context.Background(), "GET", task.URL, nil)
	if err != nil {
		s.logger.Error("Failed to create HTTP request", "task_id", task.TaskID, "url", task.URL, "error", err)
		result.Message = fmt.Sprintf("Failed to create HTTP request: %v", err)
		return s.publishResultAndAck(result, delivery)
	}
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
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result.Message = fmt.Sprintf("Received non-OK HTTP status: %d. Body preview: %s", resp.StatusCode, string(bodyBytes))
		return s.publishResultAndAck(result, delivery)
	}

	htmlBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("Failed to read response body", "task_id", task.TaskID, "url", task.URL, "error", err)
		result.Message = fmt.Sprintf("Failed to read response body: %v", err)
		return s.publishResultAndAck(result, delivery)
	}
	s.logger.Info("Successfully fetched URL content", "task_id", task.TaskID, "url", task.URL, "content_length", len(htmlBody))

	fileName := uuid.NewString() + ".html"
	objectName := fmt.Sprintf("raw/%s/%s", task.TaskID, fileName)
	s.logger.Info("Preparing to upload raw data to Minio", "task_id", task.TaskID, "object_name", objectName)

	reader := bytes.NewReader(htmlBody)
	contentType := resp.Header.Get("Content-Type")
	if mainContentType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
		contentType = mainContentType
	} else if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Если mime.ParseMediaType вернул ошибку, оставляем оригинальный contentType, если он не пустой

	uploadCtx, uploadCancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer uploadCancel()

	uploadInfo, err := s.minioClient.UploadObject(uploadCtx, objectName, reader, int64(len(htmlBody)), contentType)
	if err != nil {
		s.logger.Error("Failed to upload raw data to Minio", "task_id", task.TaskID, "object_name", objectName, "error", err)
		result.Message = fmt.Sprintf("Failed to upload raw data to Minio: %v", err)
		return s.publishResultAndAck(result, delivery)
	}
	s.logger.Info("Raw data uploaded to Minio successfully", "task_id", task.TaskID, "object_name", objectName, "etag", uploadInfo.ETag, "size", uploadInfo.Size)

	result.Success = true
	result.Message = "Successfully crawled and stored raw data"
	result.RawDataPath = objectName

	return s.publishResultAndAck(result, delivery)
}

func (s *CrawlService) publishResultAndAck(result messaging.CrawlResultEvent, delivery amqp091.Delivery) error {
	pubErr := s.publisher.Publish(context.Background(), messaging.TasksExchange, messaging.CrawlResultRoutingKey, result)
	if pubErr != nil {
		s.logger.Error("Failed to publish crawl result", "error", pubErr, "task_id", result.TaskID, "success", result.Success)
		// Логируем, но не останавливаем попытку Ack, т.к. исходное сообщение нужно обработать
	} else {
		s.logger.Info("Crawl result published", "task_id", result.TaskID, "success", result.Success, "raw_data_path", result.RawDataPath, "message", result.Message)
	}

	if ackErr := delivery.Ack(false); ackErr != nil {
		s.logger.Error("Failed to acknowledge original message", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID, "error", ackErr)
		// Не возвращаем ошибку ackErr здесь.
		// Если Ack не удался, сообщение, вероятно, будет переотправлено RabbitMQ.
		// Консьюмер, скорее всего, остановится из-за ошибки канала.
		// Но мы не хотим, чтобы rmqClient.Consume пытался сделать Nack поверх этого.
		return nil // Возвращаем nil, чтобы внешний rmqClient.Consume не пытался сделать Nack
	}
	s.logger.Debug("Original message acknowledged successfully", "delivery_tag", delivery.DeliveryTag, "task_id", result.TaskID)
	return nil
}
