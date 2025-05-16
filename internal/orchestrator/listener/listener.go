// File: internal/orchestrator/listener/listener.go
package listener

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher"
	"github.com/semantica-dev/semantica-backend/pkg/messaging"
)

type TaskListener struct {
	logger    *slog.Logger
	publisher *publisher.TaskPublisher
	// В будущем здесь может быть репозиторий для обновления статусов задач в БД
}

func NewTaskListener(logger *slog.Logger, pub *publisher.TaskPublisher) *TaskListener {
	return &TaskListener{
		logger:    logger.With("component", "task_listener"),
		publisher: pub,
	}
}

// HandleCrawlResult обрабатывает результат от Краулера и запускает Экстрактор HTML
func (l *TaskListener) HandleCrawlResult(delivery amqp091.Delivery) error {
	var crawlResult messaging.CrawlResultEvent
	if err := json.Unmarshal(delivery.Body, &crawlResult); err != nil {
		l.logger.Error("Failed to unmarshal crawl result event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	l.logger.Info("Received crawl result",
		"task_id", crawlResult.TaskID,
		"url", crawlResult.URL,
		"success", crawlResult.Success,
		"raw_data_path", crawlResult.RawDataPath)

	if !crawlResult.Success {
		l.logger.Warn("Crawl task failed, not proceeding to extraction",
			"task_id", crawlResult.TaskID, "url", crawlResult.URL, "message", crawlResult.Message)
		// TODO: Обновить статус задачи в БД как FAILED
		// TODO: Возможно, опубликовать TaskProcessingFinishedEvent с OverallSuccess: false
		return nil // Ack, так как мы обработали это состояние (неуспех)
	}

	// Запускаем задачу для Экстрактора HTML
	// В реальном приложении нужно будет решить, какой экстрактор запускать (HTML или Other)
	// на основе типа контента или пользовательского выбора.
	// Для этого MVP пока хардкодим запуск HTML экстрактора для URL.
	extractHTMLTask := messaging.ExtractHTMLTaskEvent{
		TaskID:      crawlResult.TaskID,
		OriginalURL: crawlResult.URL,
		RawDataPath: crawlResult.RawDataPath,
	}

	l.logger.Info("Publishing ExtractHTMLTaskEvent", "task_id", extractHTMLTask.TaskID, "original_url", extractHTMLTask.OriginalURL)
	// Используем метод Client() из TaskPublisher для доступа к RabbitMQClient
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLTaskRoutingKey, extractHTMLTask)
	if err != nil {
		l.logger.Error("Failed to publish ExtractHTMLTaskEvent", "error", err, "task_id", extractHTMLTask.TaskID)
		// TODO: Обработка ошибки публикации. Повтор? Запись в БД как ошибка этапа?
		return nil // Ack исходного сообщения, чтобы не блокировать очередь.
	}

	// TODO: Обновить статус задачи в БД (например, CRAWLED_SUCCESSFULLY, AWAITING_EXTRACTION)
	return nil // Ack
}

// HandleExtractHTMLResult обрабатывает результат от Экстрактора HTML и запускает Индексатор Ключевых Слов
func (l *TaskListener) HandleExtractHTMLResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractHTMLResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract html result event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	l.logger.Info("Received extract HTML result",
		"task_id", extractResult.TaskID,
		"url", extractResult.OriginalURL,
		"success", extractResult.Success,
		"markdown_path", extractResult.MarkdownPath)

	if !extractResult.Success {
		l.logger.Warn("Extract HTML task failed, not proceeding to keyword indexing",
			"task_id", extractResult.TaskID, "message", extractResult.Message)
		// TODO: Обновить статус задачи в БД как EXTRACTION_FAILED
		return nil // Ack
	}

	indexKeywordsTask := messaging.IndexKeywordsTaskEvent{
		TaskID:            extractResult.TaskID,
		OriginalURL:       extractResult.OriginalURL,
		ProcessedDataPath: extractResult.MarkdownPath, // Передаем путь к Markdown
	}

	l.logger.Info("Publishing IndexKeywordsTaskEvent", "task_id", indexKeywordsTask.TaskID)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsTaskRoutingKey, indexKeywordsTask)
	if err != nil {
		l.logger.Error("Failed to publish IndexKeywordsTaskEvent", "error", err, "task_id", indexKeywordsTask.TaskID)
		return nil
	}
	// TODO: Обновить статус задачи в БД
	return nil // Ack
}

// HandleExtractOtherResult обрабатывает результат от Экстрактора Других Файлов
// (пока не используется активно в цепочке Оркестратора, но может быть вызван, если Оркестратор начнет его слушать)
func (l *TaskListener) HandleExtractOtherResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractOtherResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract other result event", "error", err, "body", string(delivery.Body))
		return err
	}
	l.logger.Info("Received extract other result",
		"task_id", extractResult.TaskID,
		"success", extractResult.Success,
		"original_file_path", extractResult.OriginalFilePath,
		"extracted_text_path", extractResult.ExtractedTextPath)

	if !extractResult.Success {
		l.logger.Warn("Extract Other task failed", "task_id", extractResult.TaskID, "message", extractResult.Message)
		// TODO: Обновить статус задачи в БД
		return nil
	}

	// Логика для запуска следующего этапа, например, IndexKeywordsTask
	indexKeywordsTask := messaging.IndexKeywordsTaskEvent{
		TaskID:            extractResult.TaskID,
		OriginalFilePath:  extractResult.OriginalFilePath,
		ProcessedDataPath: extractResult.ExtractedTextPath,
	}
	l.logger.Info("Publishing IndexKeywordsTaskEvent (from ExtractOtherResult)", "task_id", indexKeywordsTask.TaskID)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexKeywordsTaskRoutingKey, indexKeywordsTask)
	if err != nil {
		l.logger.Error("Failed to publish IndexKeywordsTaskEvent (from ExtractOtherResult)", "error", err, "task_id", indexKeywordsTask.TaskID)
		return nil
	}
	// TODO: Обновить статус задачи в БД
	return nil
}

// HandleIndexKeywordsResult обрабатывает результат от Индексатора Ключевых Слов и запускает Индексатор Эмбеддингов
func (l *TaskListener) HandleIndexKeywordsResult(delivery amqp091.Delivery) error {
	var keywordsResult messaging.IndexKeywordsResultEvent
	if err := json.Unmarshal(delivery.Body, &keywordsResult); err != nil {
		l.logger.Error("Failed to unmarshal index keywords result event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	l.logger.Info("Received index keywords result",
		"task_id", keywordsResult.TaskID,
		"success", keywordsResult.Success,
		"keywords_stored", keywordsResult.KeywordsStored,
		"processed_data_path", keywordsResult.ProcessedDataPath)

	if !keywordsResult.Success {
		l.logger.Warn("Index keywords task failed, not proceeding to embedding indexing",
			"task_id", keywordsResult.TaskID, "message", keywordsResult.Message)
		// TODO: Обновить статус задачи в БД
		return nil // Ack
	}

	indexEmbeddingsTask := messaging.IndexEmbeddingsTaskEvent{
		TaskID:            keywordsResult.TaskID,
		OriginalURL:       keywordsResult.OriginalURL,
		OriginalFilePath:  keywordsResult.OriginalFilePath,
		ProcessedDataPath: keywordsResult.ProcessedDataPath, // Используем путь из результата предыдущего шага
	}

	l.logger.Info("Publishing IndexEmbeddingsTaskEvent", "task_id", indexEmbeddingsTask.TaskID)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.IndexEmbeddingsTaskRoutingKey, indexEmbeddingsTask)
	if err != nil {
		l.logger.Error("Failed to publish IndexEmbeddingsTaskEvent", "error", err, "task_id", indexEmbeddingsTask.TaskID)
		return nil
	}
	// TODO: Обновить статус задачи в БД
	return nil // Ack
}

// HandleIndexEmbeddingsResult обрабатывает результат от Индексатора Эмбеддингов
func (l *TaskListener) HandleIndexEmbeddingsResult(delivery amqp091.Delivery) error {
	var embeddingsResult messaging.IndexEmbeddingsResultEvent
	if err := json.Unmarshal(delivery.Body, &embeddingsResult); err != nil {
		l.logger.Error("Failed to unmarshal index embeddings result event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	l.logger.Info("Received index embeddings result",
		"task_id", embeddingsResult.TaskID,
		"success", embeddingsResult.Success,
		"embeddings_stored", embeddingsResult.EmbeddingsStored)

	if !embeddingsResult.Success {
		l.logger.Warn("Index embeddings task failed", "task_id", embeddingsResult.TaskID, "message", embeddingsResult.Message)
		// TODO: Обновить статус задачи в БД как EMBEDDING_FAILED
		return nil // Ack
	}

	l.logger.Info("Main data processing pipeline appears finished for task (after embeddings)", "task_id", embeddingsResult.TaskID)
	// Событие TaskProcessingFinishedEvent публикуется самим indexer-embeddings воркером,
	// так что здесь Оркестратору не нужно его дублировать.
	// TODO: Обновить статус задачи в БД как COMPLETED или PROCESSING_SUCCESSFUL

	return nil // Ack
}

// HandleTaskProcessingFinished обрабатывает финальное событие о завершении всей цепочки
func (l *TaskListener) HandleTaskProcessingFinished(delivery amqp091.Delivery) error {
	var finishedEvent messaging.TaskProcessingFinishedEvent
	if err := json.Unmarshal(delivery.Body, &finishedEvent); err != nil {
		l.logger.Error("Failed to unmarshal task processing finished event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	l.logger.Info("Received TaskProcessingFinishedEvent from a worker",
		"task_id", finishedEvent.TaskID,
		"overall_success", finishedEvent.OverallSuccess,
		"final_message", finishedEvent.FinalMessage,
	)
	// Здесь можно выполнить финальные действия по задаче, например, уведомить пользователя (если это роль Оркестратора)
	// или просто обновить финальный статус в БД.
	// TODO: Обновить статус задачи в БД на финальный (например, FINISHED_SUCCESS / FINISHED_FAILED).
	l.logger.Info("Processing task marked as finished", "task_id", finishedEvent.TaskID, "success", finishedEvent.OverallSuccess)
	return nil // Ack
}
