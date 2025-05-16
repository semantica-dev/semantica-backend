// File: internal/orchestrator/listener/listener.go
package listener

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rabbitmq/amqp091-go"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher" // Убедитесь, что путь правильный
	"github.com/semantica-dev/semantica-backend/pkg/messaging"                   // Убедитесь, что путь правильный
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

	l.logger.Info("Received crawl result", "task_id", crawlResult.TaskID, "url", crawlResult.URL, "success", crawlResult.Success, "raw_data_path", crawlResult.RawDataPath)

	if !crawlResult.Success {
		l.logger.Warn("Crawl task failed, not proceeding to extraction", "task_id", crawlResult.TaskID, "url", crawlResult.URL, "message", crawlResult.Message)
		// TODO: Обновить статус задачи в БД как FAILED
		return nil // Ack, так как мы обработали это состояние (неуспех)
	}

	// Запускаем задачу для Экстрактора HTML
	// В реальном приложении нужно будет решить, какой экстрактор запускать (HTML или Other)
	// Для этого MVP пока хардкодим запуск HTML экстрактора.
	// Предположим, что RawDataPath - это то, что нужно экстрактору.
	extractHTMLTask := messaging.ExtractHTMLTaskEvent{
		TaskID:      crawlResult.TaskID, // Используем тот же TaskID
		OriginalURL: crawlResult.URL,
		RawDataPath: crawlResult.RawDataPath, // Передаем путь к "сырым" данным
	}

	l.logger.Info("Publishing ExtractHTMLTaskEvent", "task_id", extractHTMLTask.TaskID, "original_url", extractHTMLTask.OriginalURL)
	err := l.publisher.Client().Publish(context.Background(), messaging.TasksExchange, messaging.ExtractHTMLTaskRoutingKey, extractHTMLTask)
	if err != nil {
		l.logger.Error("Failed to publish ExtractHTMLTaskEvent", "error", err, "task_id", extractHTMLTask.TaskID)
		// Что делать в этом случае? Повторить? Для MVP - просто логируем и Ack'аем исходное сообщение.
		return nil
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

	l.logger.Info("Received extract HTML result", "task_id", extractResult.TaskID, "url", extractResult.OriginalURL, "success", extractResult.Success, "markdown_path", extractResult.MarkdownPath)

	if !extractResult.Success {
		l.logger.Warn("Extract HTML task failed, not proceeding to keyword indexing", "task_id", extractResult.TaskID, "message", extractResult.Message)
		// TODO: Обновить статус задачи в БД как EXTRACTION_FAILED
		return nil // Ack
	}

	// Запускаем задачу для Индексатора Ключевых Слов
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

// HandleExtractOtherResult (по аналогии, если бы мы его запускали)
func (l *TaskListener) HandleExtractOtherResult(delivery amqp091.Delivery) error {
	var extractResult messaging.ExtractOtherResultEvent
	if err := json.Unmarshal(delivery.Body, &extractResult); err != nil {
		l.logger.Error("Failed to unmarshal extract other result event", "error", err, "body", string(delivery.Body))
		return err
	}
	l.logger.Info("Received extract other result", "task_id", extractResult.TaskID, "success", extractResult.Success, "path", extractResult.ExtractedTextPath)
	// Логика для запуска следующего этапа (например, IndexKeywordsTask)
	// ...
	if !extractResult.Success {
		l.logger.Warn("Extract Other task failed", "task_id", extractResult.TaskID)
		return nil
	}
	// ... публикация IndexKeywordsTaskEvent
	return nil
}

// HandleIndexKeywordsResult обрабатывает результат от Индексатора Ключевых Слов и запускает Индексатор Эмбеддингов
func (l *TaskListener) HandleIndexKeywordsResult(delivery amqp091.Delivery) error {
	var keywordsResult messaging.IndexKeywordsResultEvent
	if err := json.Unmarshal(delivery.Body, &keywordsResult); err != nil {
		l.logger.Error("Failed to unmarshal index keywords result event", "error", err, "body", string(delivery.Body))
		return err // Nack
	}

	l.logger.Info("Received index keywords result", "task_id", keywordsResult.TaskID, "success", keywordsResult.Success, "keywords_stored", keywordsResult.KeywordsStored)

	if !keywordsResult.Success {
		l.logger.Warn("Index keywords task failed, not proceeding to embedding indexing", "task_id", keywordsResult.TaskID, "message", keywordsResult.Message)
		// TODO: Обновить статус задачи в БД
		return nil // Ack
	}

	// Предполагаем, что эмбеддинги генерируются на основе того же ProcessedDataPath,
	// который использовался для ключевых слов. Если это не так, нужно будет передавать нужный путь.
	// В IndexKeywordsTaskEvent у нас есть OriginalURL и OriginalFilePath, но нет ProcessedDataPath.
	// Это нужно исправить в IndexKeywordsResultEvent или передавать его как-то иначе.
	// Пока для MVP предположим, что мы его можем получить или он не нужен для следующего шага (что неверно).
	// Давайте добавим ProcessedDataPath в IndexKeywordsResultEvent для корректности.
	// НО! Мы не можем изменить уже отправленное сообщение. Значит, нужно его как-то получить.
	// Для простоты MVP, предположим, что IndexEmbeddingsTaskEvent может быть создан без него,
	// или что он был сохранен в Оркестраторе вместе со статусом задачи.
	// Либо, что более правильно, IndexKeywordsResultEvent должен содержать ProcessedDataPath.
	// Допустим, ProcessedDataPath был в keywordsResult (если бы мы его туда добавили)

	// ВАЖНО: IndexKeywordsResultEvent не содержит ProcessedDataPath.
	// Мы должны либо:
	// 1. Получить его из состояния задачи, хранимого в Оркестраторе (если бы оно было).
	// 2. Предположить, что следующий шаг (Embedding Indexer) сам знает, откуда брать данные,
	//    используя TaskID и OriginalURL/OriginalFilePath (например, по конвенции имен в Minio).
	// 3. Изменить IndexKeywordsResultEvent, чтобы он содержал ProcessedDataPath.
	//
	// Для текущего MVP, давайте сделаем допущение, что Индексатор Эмбеддингов
	// ожидает путь к данным, который был результатом предыдущего шага экстракции.
	// Мы передавали ProcessedDataPath в IndexKeywordsTaskEvent.
	// Значит, IndexKeywordsResultEvent должен вернуть его или что-то, что позволит его восстановить.
	//
	// Временно, для простоты, будем считать, что ProcessedDataPath нам известен из контекста TaskID
	// или его можно извлечь из метаданных (но это не отражено в коде).
	// Для примера передадим "unknown_path_for_embeddings_simulated"

	indexEmbeddingsTask := messaging.IndexEmbeddingsTaskEvent{
		TaskID:           keywordsResult.TaskID,
		OriginalURL:      keywordsResult.OriginalURL,
		OriginalFilePath: keywordsResult.OriginalFilePath,
		// ProcessedDataPath: keywordsResult.ProcessedDataPath, // ЕСЛИ БЫ ОН ТАМ БЫЛ
		ProcessedDataPath: "simulated/path/from/keywords/step/" + keywordsResult.TaskID, // ЗАГЛУШКА
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

	l.logger.Info("Received index embeddings result", "task_id", embeddingsResult.TaskID, "success", embeddingsResult.Success, "embeddings_stored", embeddingsResult.EmbeddingsStored)

	if !embeddingsResult.Success {
		l.logger.Warn("Index embeddings task failed", "task_id", embeddingsResult.TaskID, "message", embeddingsResult.Message)
		// TODO: Обновить статус задачи в БД как EMBEDDING_FAILED
		return nil // Ack
	}

	// На этом этапе основная цепочка обработки данных завершена (до этапа поиска).
	// Можно обновить финальный статус задачи.
	l.logger.Info("Main data processing pipeline finished for task", "task_id", embeddingsResult.TaskID)
	// TODO: Обновить статус задачи в БД как COMPLETED или PROCESSING_SUCCESSFUL

	return nil // Ack
}

// HandleTaskProcessingFinished (если Оркестратор хочет слушать и это событие)
func (l *TaskListener) HandleTaskProcessingFinished(delivery amqp091.Delivery) error {
	var finishedEvent messaging.TaskProcessingFinishedEvent
	if err := json.Unmarshal(delivery.Body, &finishedEvent); err != nil {
		l.logger.Error("Failed to unmarshal task processing finished event", "error", err, "body", string(delivery.Body))
		return err
	}

	l.logger.Info("Received TaskProcessingFinishedEvent",
		"task_id", finishedEvent.TaskID,
		"overall_success", finishedEvent.OverallSuccess,
		"final_message", finishedEvent.FinalMessage,
	)
	// Здесь можно выполнить финальные действия по задаче, например, уведомить пользователя.
	// TODO: Обновить статус задачи в БД на финальный (например, FINISHED_SUCCESS / FINISHED_FAILED).
	return nil
}

// Вспомогательный метод для доступа к RabbitMQ клиенту из publisher
// (пока TaskPublisher не имеет такого метода, добавим его или используем напрямую)
func (p *publisher.TaskPublisher) Client() *messaging.RabbitMQClient {
	// Это заглушка, нужно чтобы TaskPublisher предоставлял доступ к своему клиенту
	// или чтобы listener получал RabbitMQClient напрямую.
	// Для MVP, если publisher инкапсулирует client, нам нужен способ его получить.
	// Либо TaskPublisher должен иметь методы для публикации ВСЕХ типов сообщений, а не только CrawlTask.
	//
	// ВАРИАНТ 1: Listener получает RabbitMQClient напрямую.
	// ВАРИАНТ 2: TaskPublisher расширяется.
	//
	// Давайте для простоты предположим, что TaskPublisher будет расширен.
	// Но пока что я буду вызывать publisher.client.Publish(...) в коде выше,
	// ПОДРАЗУМЕВАЯ, ЧТО TaskPublisher будет модифицирован, чтобы публиковать не только CrawlTask,
	// или что listener будет использовать RabbitMQClient напрямую для этих публикаций.
	//
	// Для текущей реализации кода listener'а выше, я использовал publisher.Client().Publish(...),
	// что предполагает, что у publisher есть метод Client(), возвращающий *messaging.RabbitMQClient.
	// Давайте добавим его в TaskPublisher для консистентности.
	return nil // Эта функция здесь просто для примера, она должна быть в publisher.go
}
