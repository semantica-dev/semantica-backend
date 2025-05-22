// File: internal/orchestrator/listener/listener.go
package listener

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type TaskListener struct {
	logger    *slog.Logger
	publisher *publisher.TaskPublisher
}

func NewTaskListener(logger *slog.Logger, pub *publisher.TaskPublisher) *TaskListener {
	return &TaskListener{
		logger:    logger.With("component", "task_listener"),
		publisher: pub,
	}
}

// ackDelivery подтверждает доставку сообщения и логирует результат.
// Возвращает ошибку, если подтверждение не удалось.
func (l *TaskListener) ackDelivery(d amqp091.Delivery, taskID string, contextMsg string) error {
	l.logger.Debug(fmt.Sprintf("Attempting to acknowledge message for %s", contextMsg), "delivery_tag", d.DeliveryTag, "task_id", taskID)
	if err := d.Ack(false); err != nil {
		l.logger.Error(fmt.Sprintf("Failed to acknowledge message for %s", contextMsg), "delivery_tag", d.DeliveryTag, "task_id", taskID, "error", err)
		return fmt.Errorf("failed to Ack message (tag %d) for %s, task_id %s: %w", d.DeliveryTag, contextMsg, taskID, err)
	}
	l.logger.Info(fmt.Sprintf("Successfully acknowledged message for %s", contextMsg), "delivery_tag", d.DeliveryTag, "task_id", taskID)
	return nil
}

func (l *TaskListener) HandleCrawlResult(delivery amqp091.Delivery) error {
	var crawlResult messaging.CrawlResultEvent
	if err := json.Unmarshal(delivery.Body, &crawlResult); err != nil {
		l.logger.Error("Failed to unmarshal crawl result event", "error", err, "body", string(delivery.Body))
		// Сообщение не может быть обработано, возвращаем ошибку, чтобы rmqClient.Consume ее вернул
		// Это приведет к перезапуску консьюмера. Сообщение останется в очереди или уйдет в DLX (если настроено).
		return fmt.Errorf("unmarshal CrawlResultEvent: %w", err)
	}
	logCtx := l.logger.With("task_id", crawlResult.TaskID, "url", crawlResult.URL)
	logCtx.Info("Received crawl result", "success", crawlResult.Success, "raw_data_path", crawlResult.RawDataPath)

	if !crawlResult.Success {
		logCtx.Warn("Crawl task failed, not proceeding to extraction", "message", crawlResult.Message)
		// TODO: Обновить статус задачи в БД как FAILED
		return l.ackDelivery(delivery, crawlResult.TaskID, "failed crawl task")
	}

	extractHTMLTask := messaging.ExtractHTMLTaskEvent{
		TaskID:      crawlResult.TaskID,
		OriginalURL: crawlResult.URL,
		RawDataPath: crawlResult.RawDataPath,
	}

	logCtx.Info("Publishing ExtractHTMLTaskEvent", "original_url", extractHTMLTask.OriginalURL)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLTaskRoutingKey, extractHTMLTask)
	if err != nil {
		logCtx.Error("Failed to publish ExtractHTMLTaskEvent", "error", err)
		// Публикация не удалась. Ack'аем исходное сообщение, чтобы оно не блокировало очередь. Задача "зависнет".
		// TODO: Записать в БД о сбое этапа.
		return l.ackDelivery(delivery, crawlResult.TaskID, "crawl task after failing to publish next step")
	}

	// TODO: Обновить статус задачи в БД (например, CRAWLED_SUCCESSFULLY, AWAITING_EXTRACTION)
	return l.ackDelivery(delivery, crawlResult.TaskID, "successful crawl task processing")
}

func (l *TaskListener) HandleExtractHTMLResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractHTMLResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract html result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractHTMLResultEvent: %w", err)
	}
	logCtx := l.logger.With("task_id", extractResult.TaskID, "url", extractResult.OriginalURL)
	logCtx.Info("Received extract HTML result", "success", extractResult.Success, "markdown_path", extractResult.MarkdownPath)

	if !extractResult.Success {
		logCtx.Warn("Extract HTML task failed, not proceeding", "message", extractResult.Message)
		return l.ackDelivery(delivery, extractResult.TaskID, "failed extract HTML task")
	}

	indexKeywordsTask := messaging.IndexKeywordsTaskEvent{
		TaskID:            extractResult.TaskID,
		OriginalURL:       extractResult.OriginalURL,
		ProcessedDataPath: extractResult.MarkdownPath,
	}
	logCtx.Info("Publishing IndexKeywordsTaskEvent")
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsTaskRoutingKey, indexKeywordsTask)
	if err != nil {
		logCtx.Error("Failed to publish IndexKeywordsTaskEvent", "error", err)
		return l.ackDelivery(delivery, extractResult.TaskID, "extract HTML task after failing to publish next step")
	}
	return l.ackDelivery(delivery, extractResult.TaskID, "successful extract HTML task processing")
}

func (l *TaskListener) HandleExtractOtherResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractOtherResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract other result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractOtherResultEvent: %w", err)
	}
	logCtx := l.logger.With("task_id", extractResult.TaskID, "original_file_path", extractResult.OriginalFilePath)
	logCtx.Info("Received extract other result", "success", extractResult.Success, "extracted_text_path", extractResult.ExtractedTextPath)

	if !extractResult.Success {
		logCtx.Warn("Extract Other task failed", "message", extractResult.Message)
		return l.ackDelivery(delivery, extractResult.TaskID, "failed extract other task")
	}

	indexKeywordsTask := messaging.IndexKeywordsTaskEvent{
		TaskID:            extractResult.TaskID,
		OriginalFilePath:  extractResult.OriginalFilePath,
		ProcessedDataPath: extractResult.ExtractedTextPath,
	}
	logCtx.Info("Publishing IndexKeywordsTaskEvent (from ExtractOtherResult)")
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsTaskRoutingKey, indexKeywordsTask)
	if err != nil {
		logCtx.Error("Failed to publish IndexKeywordsTaskEvent (from ExtractOtherResult)", "error", err)
		return l.ackDelivery(delivery, extractResult.TaskID, "extract other task after failing to publish next step")
	}
	return l.ackDelivery(delivery, extractResult.TaskID, "successful extract other task processing")
}

func (l *TaskListener) HandleIndexKeywordsResult(delivery amqp091.Delivery) error {
	var keywordsResult messaging.IndexKeywordsResultEvent
	if err := json.Unmarshal(delivery.Body, &keywordsResult); err != nil {
		l.logger.Error("Failed to unmarshal index keywords result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal IndexKeywordsResultEvent: %w", err)
	}
	logCtx := l.logger.With("task_id", keywordsResult.TaskID)
	logCtx.Info("Received index keywords result", "success", keywordsResult.Success, "keywords_stored", keywordsResult.KeywordsStored, "processed_data_path", keywordsResult.ProcessedDataPath)

	if !keywordsResult.Success {
		logCtx.Warn("Index keywords task failed, not proceeding", "message", keywordsResult.Message)
		return l.ackDelivery(delivery, keywordsResult.TaskID, "failed index keywords task")
	}

	indexEmbeddingsTask := messaging.IndexEmbeddingsTaskEvent{
		TaskID:            keywordsResult.TaskID,
		OriginalURL:       keywordsResult.OriginalURL,
		OriginalFilePath:  keywordsResult.OriginalFilePath,
		ProcessedDataPath: keywordsResult.ProcessedDataPath,
	}
	logCtx.Info("Publishing IndexEmbeddingsTaskEvent")
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexEmbeddingsTaskRoutingKey, indexEmbeddingsTask)
	if err != nil {
		logCtx.Error("Failed to publish IndexEmbeddingsTaskEvent", "error", err)
		return l.ackDelivery(delivery, keywordsResult.TaskID, "index keywords task after failing to publish next step")
	}
	return l.ackDelivery(delivery, keywordsResult.TaskID, "successful index keywords task processing")
}

func (l *TaskListener) HandleIndexEmbeddingsResult(delivery amqp091.Delivery) error {
	var embeddingsResult messaging.IndexEmbeddingsResultEvent
	if err := json.Unmarshal(delivery.Body, &embeddingsResult); err != nil {
		l.logger.Error("Failed to unmarshal index embeddings result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal IndexEmbeddingsResultEvent: %w", err)
	}
	logCtx := l.logger.With("task_id", embeddingsResult.TaskID)
	logCtx.Info("Received index embeddings result", "success", embeddingsResult.Success, "embeddings_stored", embeddingsResult.EmbeddingsStored)

	if !embeddingsResult.Success {
		logCtx.Warn("Index embeddings task failed", "message", embeddingsResult.Message)
		return l.ackDelivery(delivery, embeddingsResult.TaskID, "failed index embeddings task")
	}

	logCtx.Info("Main data processing pipeline appears finished for task (after embeddings)")
	// Событие TaskProcessingFinishedEvent публикуется самим indexer-embeddings воркером.
	return l.ackDelivery(delivery, embeddingsResult.TaskID, "successful index embeddings task processing")
}

func (l *TaskListener) HandleTaskProcessingFinished(delivery amqp091.Delivery) error {
	var finishedEvent messaging.TaskProcessingFinishedEvent
	if err := json.Unmarshal(delivery.Body, &finishedEvent); err != nil {
		l.logger.Error("Failed to unmarshal task processing finished event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal TaskProcessingFinishedEvent: %w", err)
	}
	logCtx := l.logger.With("task_id", finishedEvent.TaskID)
	logCtx.Info("Received TaskProcessingFinishedEvent from a worker", "overall_success", finishedEvent.OverallSuccess, "final_message", finishedEvent.FinalMessage)

	// TODO: Обновить статус задачи в БД на финальный (например, FINISHED_SUCCESS / FINISHED_FAILED).
	logCtx.Info("Processing task marked as finished by worker event.")
	return l.ackDelivery(delivery, finishedEvent.TaskID, "task processing finished event")
}
