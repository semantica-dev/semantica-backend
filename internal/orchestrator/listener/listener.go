// File: internal/orchestrator/listener/listener.go
package listener

import (
	"context"
	"encoding/json"
	"fmt" // Добавлен для возврата ошибок
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

func (l *TaskListener) HandleCrawlResult(delivery amqp091.Delivery) error {
	var crawlResult messaging.CrawlResultEvent
	if err := json.Unmarshal(delivery.Body, &crawlResult); err != nil {
		l.logger.Error("Failed to unmarshal crawl result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal CrawlResultEvent: %w", err) // Возвращаем ошибку для Nack/перезапуска
	}

	l.logger.Info("Received crawl result", "task_id", crawlResult.TaskID, "url", crawlResult.URL, "success", crawlResult.Success, "raw_data_path", crawlResult.RawDataPath)

	if !crawlResult.Success {
		l.logger.Warn("Crawl task failed, not proceeding to extraction", "task_id", crawlResult.TaskID, "url", crawlResult.URL, "message", crawlResult.Message)
		l.logger.Debug("HandleCrawlResult: Processing finished (task failed), returning nil for Ack.", "task_id", crawlResult.TaskID)
		return nil // Ack, так как мы обработали это состояние (неуспех)
	}

	extractHTMLTask := messaging.ExtractHTMLTaskEvent{
		TaskID:      crawlResult.TaskID,
		OriginalURL: crawlResult.URL,
		RawDataPath: crawlResult.RawDataPath,
	}

	l.logger.Info("Publishing ExtractHTMLTaskEvent", "task_id", extractHTMLTask.TaskID, "original_url", extractHTMLTask.OriginalURL)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLTaskRoutingKey, extractHTMLTask)
	if err != nil {
		l.logger.Error("Failed to publish ExtractHTMLTaskEvent", "error", err, "task_id", extractHTMLTask.TaskID)
		// Если публикация не удалась, это ошибка этапа. Сообщение будет Ack'нуто, но задача "застрянет".
		// В будущем здесь нужна лучшая обработка, например, запись в БД о сбое этапа.
		l.logger.Debug("HandleCrawlResult: Failed to publish next task, but returning nil for Ack of current message.", "task_id", crawlResult.TaskID)
		return nil
	}
	l.logger.Debug("HandleCrawlResult: Processing finished successfully, returning nil for Ack.", "task_id", crawlResult.TaskID)
	return nil
}

// Аналогичные изменения для других Handle...Result методов в listener.go:
// - Возвращать ошибку при анмаршалинге.
// - Логировать перед return nil.

func (l *TaskListener) HandleExtractHTMLResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractHTMLResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract html result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractHTMLResultEvent: %w", err)
	}
	l.logger.Info("Received extract HTML result", "task_id", extractResult.TaskID, "url", extractResult.OriginalURL, "success", extractResult.Success, "markdown_path", extractResult.MarkdownPath)
	if !extractResult.Success {
		l.logger.Warn("Extract HTML task failed, not proceeding", "task_id", extractResult.TaskID, "message", extractResult.Message)
		l.logger.Debug("HandleExtractHTMLResult: Processing finished (task failed), returning nil for Ack.", "task_id", extractResult.TaskID)
		return nil
	}
	indexKeywordsTask := messaging.IndexKeywordsTaskEvent{
		TaskID:            extractResult.TaskID,
		OriginalURL:       extractResult.OriginalURL,
		ProcessedDataPath: extractResult.MarkdownPath,
	}
	l.logger.Info("Publishing IndexKeywordsTaskEvent", "task_id", indexKeywordsTask.TaskID)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsTaskRoutingKey, indexKeywordsTask)
	if err != nil {
		l.logger.Error("Failed to publish IndexKeywordsTaskEvent", "error", err, "task_id", indexKeywordsTask.TaskID)
		l.logger.Debug("HandleExtractHTMLResult: Failed to publish next task, but returning nil for Ack of current message.", "task_id", extractResult.TaskID)
		return nil
	}
	l.logger.Debug("HandleExtractHTMLResult: Processing finished successfully, returning nil for Ack.", "task_id", extractResult.TaskID)
	return nil
}

func (l *TaskListener) HandleExtractOtherResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractOtherResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract other result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal ExtractOtherResultEvent: %w", err)
	}
	l.logger.Info("Received extract other result", "task_id", extractResult.TaskID, "success", extractResult.Success, "original_file_path", extractResult.OriginalFilePath, "extracted_text_path", extractResult.ExtractedTextPath)
	if !extractResult.Success {
		l.logger.Warn("Extract Other task failed", "task_id", extractResult.TaskID, "message", extractResult.Message)
		l.logger.Debug("HandleExtractOtherResult: Processing finished (task failed), returning nil for Ack.", "task_id", extractResult.TaskID)
		return nil
	}
	indexKeywordsTask := messaging.IndexKeywordsTaskEvent{
		TaskID:            extractResult.TaskID,
		OriginalFilePath:  extractResult.OriginalFilePath,
		ProcessedDataPath: extractResult.ExtractedTextPath,
	}
	l.logger.Info("Publishing IndexKeywordsTaskEvent (from ExtractOtherResult)", "task_id", indexKeywordsTask.TaskID)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsTaskRoutingKey, indexKeywordsTask)
	if err != nil {
		l.logger.Error("Failed to publish IndexKeywordsTaskEvent (from ExtractOtherResult)", "error", err, "task_id", indexKeywordsTask.TaskID)
		l.logger.Debug("HandleExtractOtherResult: Failed to publish next task, but returning nil for Ack of current message.", "task_id", extractResult.TaskID)
		return nil
	}
	l.logger.Debug("HandleExtractOtherResult: Processing finished successfully, returning nil for Ack.", "task_id", extractResult.TaskID)
	return nil
}

func (l *TaskListener) HandleIndexKeywordsResult(delivery amqp091.Delivery) error {
	var keywordsResult messaging.IndexKeywordsResultEvent
	if err := json.Unmarshal(delivery.Body, &keywordsResult); err != nil {
		l.logger.Error("Failed to unmarshal index keywords result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal IndexKeywordsResultEvent: %w", err)
	}
	l.logger.Info("Received index keywords result", "task_id", keywordsResult.TaskID, "success", keywordsResult.Success, "keywords_stored", keywordsResult.KeywordsStored, "processed_data_path", keywordsResult.ProcessedDataPath)
	if !keywordsResult.Success {
		l.logger.Warn("Index keywords task failed, not proceeding", "task_id", keywordsResult.TaskID, "message", keywordsResult.Message)
		l.logger.Debug("HandleIndexKeywordsResult: Processing finished (task failed), returning nil for Ack.", "task_id", keywordsResult.TaskID)
		return nil
	}
	indexEmbeddingsTask := messaging.IndexEmbeddingsTaskEvent{
		TaskID:            keywordsResult.TaskID,
		OriginalURL:       keywordsResult.OriginalURL,
		OriginalFilePath:  keywordsResult.OriginalFilePath,
		ProcessedDataPath: keywordsResult.ProcessedDataPath,
	}
	l.logger.Info("Publishing IndexEmbeddingsTaskEvent", "task_id", indexEmbeddingsTask.TaskID)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexEmbeddingsTaskRoutingKey, indexEmbeddingsTask)
	if err != nil {
		l.logger.Error("Failed to publish IndexEmbeddingsTaskEvent", "error", err, "task_id", indexEmbeddingsTask.TaskID)
		l.logger.Debug("HandleIndexKeywordsResult: Failed to publish next task, but returning nil for Ack of current message.", "task_id", keywordsResult.TaskID)
		return nil
	}
	l.logger.Debug("HandleIndexKeywordsResult: Processing finished successfully, returning nil for Ack.", "task_id", keywordsResult.TaskID)
	return nil
}

func (l *TaskListener) HandleIndexEmbeddingsResult(delivery amqp091.Delivery) error {
	var embeddingsResult messaging.IndexEmbeddingsResultEvent
	if err := json.Unmarshal(delivery.Body, &embeddingsResult); err != nil {
		l.logger.Error("Failed to unmarshal index embeddings result event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal IndexEmbeddingsResultEvent: %w", err)
	}
	l.logger.Info("Received index embeddings result", "task_id", embeddingsResult.TaskID, "success", embeddingsResult.Success, "embeddings_stored", embeddingsResult.EmbeddingsStored)
	if !embeddingsResult.Success {
		l.logger.Warn("Index embeddings task failed", "task_id", embeddingsResult.TaskID, "message", embeddingsResult.Message)
		l.logger.Debug("HandleIndexEmbeddingsResult: Processing finished (task failed), returning nil for Ack.", "task_id", embeddingsResult.TaskID)
		return nil
	}
	l.logger.Info("Main data processing pipeline appears finished for task (after embeddings)", "task_id", embeddingsResult.TaskID)
	l.logger.Debug("HandleIndexEmbeddingsResult: Processing finished successfully, returning nil for Ack.", "task_id", embeddingsResult.TaskID)
	return nil
}

func (l *TaskListener) HandleTaskProcessingFinished(delivery amqp091.Delivery) error {
	var finishedEvent messaging.TaskProcessingFinishedEvent
	if err := json.Unmarshal(delivery.Body, &finishedEvent); err != nil {
		l.logger.Error("Failed to unmarshal task processing finished event", "error", err, "body", string(delivery.Body))
		return fmt.Errorf("unmarshal TaskProcessingFinishedEvent: %w", err)
	}
	l.logger.Info("Received TaskProcessingFinishedEvent from a worker", "task_id", finishedEvent.TaskID, "overall_success", finishedEvent.OverallSuccess, "final_message", finishedEvent.FinalMessage)
	l.logger.Info("Processing task marked as finished", "task_id", finishedEvent.TaskID, "success", finishedEvent.OverallSuccess)
	l.logger.Debug("HandleTaskProcessingFinished: Processing finished, returning nil for Ack.", "task_id", finishedEvent.TaskID)
	return nil
}
