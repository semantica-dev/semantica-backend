// File: pkg/messaging/rabbitmq.go
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQClient struct {
	conn          *amqp091.Connection
	logger        *slog.Logger
	amqpURL       string // Сохраняем URL для возможности переподключения
	maxRetries    int
	retryInterval time.Duration
	// Канал (channel) удален из структуры, будет управляться локально в методах
}

// NewRabbitMQClient создает нового клиента RabbitMQ, устанавливая соединение.
func NewRabbitMQClient(amqpURL string, logger *slog.Logger, maxRetries int, retryInterval time.Duration) (*RabbitMQClient, error) {
	client := &RabbitMQClient{
		logger:        logger,
		amqpURL:       amqpURL,
		maxRetries:    maxRetries,
		retryInterval: retryInterval,
	}
	// Устанавливаем соединение при создании клиента
	if err := client.connect(); err != nil {
		return nil, fmt.Errorf("initial connection failed: %w", err)
	}
	return client, nil
}

// connect устанавливает или переустанавливает соединение с RabbitMQ.
func (c *RabbitMQClient) connect() error {
	var conn *amqp091.Connection
	var err error

	// Закрываем существующее соединение, если оно есть и открыто
	if c.conn != nil && !c.conn.IsClosed() {
		c.logger.Debug("Closing existing connection before reconnecting.")
		// Ошибку закрытия здесь можно проигнорировать или только залогировать,
		// так как мы все равно пытаемся установить новое соединение.
		_ = c.conn.Close()
	}
	c.conn = nil // Сбрасываем старое соединение

	actualMaxRetries := c.maxRetries
	if actualMaxRetries <= 0 {
		actualMaxRetries = 1
	}

	for attempt := 1; attempt <= actualMaxRetries; attempt++ {
		c.logger.Info("Attempting to connect to RabbitMQ...", "attempt", attempt, "max_attempts", actualMaxRetries, "url", c.amqpURL)
		conn, err = amqp091.Dial(c.amqpURL)
		if err == nil {
			c.logger.Info("Successfully connected to RabbitMQ", "attempt", attempt)
			c.conn = conn // Сохраняем новое соединение

			// Опционально: настроить слушателя NotifyClose для автоматического переподключения
			// go c.handleConnectionClose(c.conn)
			return nil
		}
		c.logger.Warn("Failed to connect to RabbitMQ", "attempt", attempt, "error", err)
		if attempt < actualMaxRetries {
			c.logger.Info("Retrying after interval", "interval", c.retryInterval.String())
			time.Sleep(c.retryInterval)
		}
	}
	return fmt.Errorf("failed to connect to RabbitMQ after %d attempts: %w", actualMaxRetries, err)
}

/*
// handleConnectionClose можно будет реализовать позже для автоматического реконнекта
func (c *RabbitMQClient) handleConnectionClose(oldConn *amqp091.Connection) {
	notifyClose := make(chan *amqp091.Error)
	oldConn.NotifyClose(notifyClose)

	err := <-notifyClose // Блокируется до закрытия соединения
	if err != nil {
		c.logger.Warn("RabbitMQ connection closed with error, attempting to reconnect...", "error", err)
		// Попытка переподключения в фоне
		go func() {
			if reconnErr := c.connect(); reconnErr != nil {
				c.logger.Error("Failed to automatically reconnect to RabbitMQ", "error", reconnErr)
			} else {
				c.logger.Info("Successfully reconnected to RabbitMQ automatically.")
			}
		}()
	} else {
		c.logger.Info("RabbitMQ connection closed gracefully (or without error).")
	}
}
*/

// getChannel открывает новый канал. Вызывающий код должен закрыть канал.
func (c *RabbitMQClient) getChannel() (*amqp091.Channel, error) {
	if c.IsClosed() { // Проверяем соединение
		c.logger.Warn("Connection is closed, attempting to reconnect before getting channel.")
		if err := c.connect(); err != nil { // Пытаемся переподключиться
			return nil, fmt.Errorf("failed to reconnect to get channel: %w", err)
		}
	}
	// Соединение должно быть открыто после c.connect() или если оно не было закрыто
	if c.conn == nil { // Дополнительная проверка на всякий случай
		return nil, fmt.Errorf("cannot get channel, connection is nil")
	}

	ch, err := c.conn.Channel()
	if err != nil {
		// Если не удалось открыть канал, возможно, соединение "испорчено"
		// Попробуем переподключиться и снова получить канал
		c.logger.Warn("Failed to open channel on existing connection, trying to reconnect and get channel again", "error", err)
		if reconnErr := c.connect(); reconnErr != nil {
			return nil, fmt.Errorf("failed to reconnect after failing to open channel: %w (original channel error: %v)", reconnErr, err)
		}
		// Повторная попытка открыть канал на новом соединении
		ch, err = c.conn.Channel()
		if err != nil {
			return nil, fmt.Errorf("failed to open a channel even after reconnect: %w", err)
		}
	}
	c.logger.Debug("Successfully obtained a new channel.")
	return ch, nil
}

// Publish публикует сообщение в RabbitMQ, используя временный канал.
func (c *RabbitMQClient) Publish(ctx context.Context, exchange, routingKey string, body interface{}) error {
	ch, err := c.getChannel() // Получаем новый канал
	if err != nil {
		c.logger.Error("Failed to get channel for publishing", "error", err, "exchange", exchange, "routing_key", routingKey)
		return fmt.Errorf("publish: failed to get channel: %w", err)
	}
	defer func() {
		if err := ch.Close(); err != nil {
			c.logger.Warn("Failed to close temporary publishing channel", "error", err)
		}
	}()

	// Объявляем Exchange на этом канале (идемпотентно)
	// Важно делать это на каждом новом канале, если нет уверенности, что exchange уже есть
	// или если политики сервера могут удалять его.
	// Для topic exchange это обычно безопасно.
	err = ch.ExchangeDeclare(
		exchange,
		amqp091.ExchangeTopic, // Можно сделать тип exchange параметром, если нужно
		true,                  // durable
		false,                 // auto-deleted
		false,                 // internal
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		c.logger.Error("Failed to declare exchange for publishing on temporary channel", "error", err, "exchange_name", exchange)
		return fmt.Errorf("publish: failed to declare exchange '%s': %w", exchange, err)
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		c.logger.Error("Failed to marshal body to JSON for publishing", "error", err, "routing_key", routingKey)
		return err
	}

	msg := amqp091.Publishing{
		ContentType:  "application/json",
		Body:         jsonBody,
		DeliveryMode: amqp091.Persistent,
	}

	publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second) // Увеличим таймаут для надежности
	defer cancel()

	err = ch.PublishWithContext(publishCtx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		msg,
	)
	if err != nil {
		c.logger.Error("Failed to publish a message", "error", err, "exchange", exchange, "routing_key", routingKey)
		return err
	}
	c.logger.Debug("Message published successfully", "exchange", exchange, "routing_key", routingKey, "body_length", len(jsonBody))
	return nil
}

// ConsumeOpts содержит опции для консьюмера.
type ConsumeOpts struct {
	QueueName    string
	ExchangeName string
	RoutingKey   string
	ConsumerTag  string
}

// Consume начинает прослушивание очереди и вызывает handler для каждого сообщения.
// Использует новый канал для каждой сессии Consume.
// Handler сам отвечает за Ack/Nack сообщения. Если handler возвращает ошибку,
// Consume также возвращает эту ошибку.
func (c *RabbitMQClient) Consume(opts ConsumeOpts, handler func(delivery amqp091.Delivery) error) error {
	ch, err := c.getChannel() // Получаем новый канал для этого консьюмера
	if err != nil {
		c.logger.Error("Consumer: failed to get channel", "error", err, "queue_name", opts.QueueName, "consumer_tag", opts.ConsumerTag)
		return err // Возвращаем ошибку, чтобы цикл перезапуска в main сработал
	}
	c.logger.Info("Consumer: channel obtained", "queue_name", opts.QueueName, "consumer_tag", opts.ConsumerTag)

	// defer ch.Close() должен быть вызван, когда горутина, использующая этот Consume, завершается,
	// или когда Consume сам выходит из цикла.
	// Если Consume возвращает ошибку, канал должен быть закрыт.
	// Если Consume выходит из-за закрытия msgs (например, при rmqClient.Close()), канал тоже должен быть закрыт.
	var consumeErr error
	defer func() {
		c.logger.Info("Consumer: closing channel...", "queue_name", opts.QueueName, "consumer_tag", opts.ConsumerTag)
		if err := ch.Close(); err != nil {
			c.logger.Warn("Consumer: failed to close channel", "error", err, "queue_name", opts.QueueName, "consumer_tag", opts.ConsumerTag)
		} else {
			c.logger.Info("Consumer: channel closed.", "queue_name", opts.QueueName, "consumer_tag", opts.ConsumerTag)
		}
	}()

	q, err := ch.QueueDeclare(
		opts.QueueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		consumeErr = fmt.Errorf("consumer: failed to declare queue '%s': %w", opts.QueueName, err)
		c.logger.Error(consumeErr.Error())
		return consumeErr
	}
	c.logger.Info("Consumer: queue declared", "queue_name", q.Name, "messages", q.Messages, "consumers", q.Consumers, "consumer_tag", opts.ConsumerTag)

	// Объявляем Exchange перед биндингом (на всякий случай, если он еще не существует)
	// Это идемпотентная операция.
	err = ch.ExchangeDeclare(
		opts.ExchangeName,
		amqp091.ExchangeTopic, // Предполагаем Topic, можно сделать параметром
		true,                  // durable
		false,                 // auto-deleted
		false,                 // internal
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		consumeErr = fmt.Errorf("consumer: failed to declare exchange '%s' for binding: %w", opts.ExchangeName, err)
		c.logger.Error(consumeErr.Error(), "consumer_tag", opts.ConsumerTag)
		return consumeErr
	}

	err = ch.QueueBind(
		q.Name,
		opts.RoutingKey,
		opts.ExchangeName,
		false,
		nil,
	)
	if err != nil {
		consumeErr = fmt.Errorf("consumer: failed to bind queue '%s' to exchange '%s' with key '%s': %w", q.Name, opts.ExchangeName, opts.RoutingKey, err)
		c.logger.Error(consumeErr.Error(), "consumer_tag", opts.ConsumerTag)
		return consumeErr
	}
	c.logger.Info("Consumer: queue bound", "queue_name", q.Name, "exchange", opts.ExchangeName, "routing_key", opts.RoutingKey, "consumer_tag", opts.ConsumerTag)

	if err := ch.Qos(
		1,     // prefetchCount
		0,     // prefetchSize
		false, // global
	); err != nil {
		consumeErr = fmt.Errorf("consumer: failed to set QoS for queue '%s': %w", q.Name, err)
		c.logger.Error(consumeErr.Error(), "consumer_tag", opts.ConsumerTag)
		return consumeErr
	}
	c.logger.Info("Consumer: QoS set", "queue_name", q.Name, "prefetch_count", 1, "consumer_tag", opts.ConsumerTag)

	msgs, err := ch.Consume(
		q.Name,
		opts.ConsumerTag,
		false, // auto-ack (ручное подтверждение)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		consumeErr = fmt.Errorf("consumer: failed to register consumer for queue '%s': %w", q.Name, err)
		c.logger.Error(consumeErr.Error(), "consumer_tag", opts.ConsumerTag)
		return consumeErr
	}
	c.logger.Info("Consumer registered, waiting for messages...", "queue_name", q.Name, "consumer_tag", opts.ConsumerTag)

	// Горутина для отслеживания закрытия канала msgs или самого канала ch
	// Это поможет корректно выйти из цикла for d := range msgs, если канал закроется со стороны сервера
	doneConsuming := make(chan error)
	go func() {
		for d := range msgs {
			c.logger.Debug("Consumer: received a message", "delivery_tag", d.DeliveryTag, "body_length", len(d.Body), "consumer_tag", opts.ConsumerTag)
			processErr := handler(d) // handler отвечает за Ack
			if processErr != nil {
				c.logger.Error("Consumer: handler returned error for message. Consumer will stop and attempt restart.",
					"error", processErr, "delivery_tag", d.DeliveryTag, "consumer_tag", opts.ConsumerTag)
				doneConsuming <- processErr // Отправляем ошибку, чтобы основной поток Consume завершился
				return                      // Выходим из горутины обработки сообщений
			}
			c.logger.Debug("Consumer: handler processed message successfully", "delivery_tag", d.DeliveryTag, "consumer_tag", opts.ConsumerTag)
		}
		c.logger.Info("Consumer: message delivery channel (msgs) closed.", "consumer_tag", opts.ConsumerTag, "queue_name", opts.QueueName)
		doneConsuming <- nil // Сигнализируем о штатном закрытии msgs
	}()

	// Ожидаем либо ошибки от обработки сообщений, либо закрытия канала со стороны RabbitMQ
	notifyChanClose := make(chan *amqp091.Error)
	ch.NotifyClose(notifyChanClose)

	select {
	case errFromHandler := <-doneConsuming: // Ошибка от обработчика или штатное закрытие msgs
		if errFromHandler != nil {
			c.logger.Error("Consumer: stopping due to handler error.", "error", errFromHandler, "consumer_tag", opts.ConsumerTag)
			return errFromHandler
		}
		c.logger.Info("Consumer: stopping because message delivery channel closed gracefully.", "consumer_tag", opts.ConsumerTag)
		return nil // msgs закрыт, выходим штатно

	case errChanClosed := <-notifyChanClose: // Канал AMQP был закрыт сервером
		c.logger.Warn("Consumer: AMQP channel closed by server.", "error", errChanClosed, "consumer_tag", opts.ConsumerTag)
		// Канал уже закрыт, defer ch.Close() не должен вызвать проблем.
		// Возвращаем ошибку, чтобы инициировать перезапуск консьюмера.
		if errChanClosed != nil {
			return fmt.Errorf("consumer: amqp channel closed by server: %w", errChanClosed)
		}
		return fmt.Errorf("consumer: amqp channel closed by server (no specific error)")
	}
}

// Close закрывает соединение RabbitMQ.
func (c *RabbitMQClient) Close() {
	if c.conn != nil && !c.conn.IsClosed() {
		c.logger.Info("Closing RabbitMQ connection...")
		err := c.conn.Close()
		if err != nil {
			c.logger.Warn("Error closing RabbitMQ connection", "error", err)
		} else {
			c.logger.Info("RabbitMQ connection closed successfully.")
		}
	}
	c.conn = nil // Убедимся, что соединение помечено как отсутствующее
}

// IsClosed проверяет, закрыто ли соединение с RabbitMQ.
func (c *RabbitMQClient) IsClosed() bool {
	if c.conn == nil {
		return true
	}
	return c.conn.IsClosed()
}
