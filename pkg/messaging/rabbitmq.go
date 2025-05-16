// File: pkg/messaging/rabbitmq.go
package messaging

import (
	"context"
	"encoding/json"
	"fmt" // Добавим fmt для форматирования ошибок
	"log/slog"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQClient struct {
	conn    *amqp091.Connection
	channel *amqp091.Channel
	logger  *slog.Logger
}

// NewRabbitMQClient создает нового клиента RabbitMQ с механизмом повторных попыток подключения.
// amqpURL: строка подключения.
// logger: экземпляр slog.Logger.
// maxRetries: максимальное количество попыток подключения.
// retryInterval: интервал между попытками.
func NewRabbitMQClient(amqpURL string, logger *slog.Logger, maxRetries int, retryInterval time.Duration) (*RabbitMQClient, error) {
	var conn *amqp091.Connection
	var err error

	if maxRetries <= 0 {
		maxRetries = 1 // Хотя бы одна попытка
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Info("Attempting to connect to RabbitMQ...", "attempt", attempt, "max_attempts", maxRetries, "url", amqpURL)
		conn, err = amqp091.Dial(amqpURL)
		if err == nil {
			// Успешное подключение
			logger.Info("Successfully connected to RabbitMQ", "attempt", attempt)
			break // Выходим из цикла
		}

		logger.Warn("Failed to connect to RabbitMQ", "attempt", attempt, "error", err)
		if attempt < maxRetries {
			logger.Info("Retrying after interval", "interval", retryInterval.String())
			time.Sleep(retryInterval)
		}
	}

	if err != nil { // Если после всех попыток ошибка осталась
		logger.Error("Failed to connect to RabbitMQ after multiple retries", "max_attempts", maxRetries, "error", err)
		return nil, fmt.Errorf("failed to connect to RabbitMQ after %d attempts: %w", maxRetries, err)
	}

	// Если подключение успешно, открываем канал
	ch, err := conn.Channel()
	if err != nil {
		conn.Close() // Закрываем соединение, если не удалось открыть канал
		logger.Error("Failed to open a RabbitMQ channel", "error", err)
		return nil, fmt.Errorf("failed to open RabbitMQ channel: %w", err)
	}
	logger.Info("RabbitMQ channel opened successfully")

	// Объявляем Exchange, если он еще не существует
	err = ch.ExchangeDeclare(
		TasksExchange,         // name
		amqp091.ExchangeTopic, // type
		true,                  // durable
		false,                 // auto-deleted
		false,                 // internal
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		logger.Error("Failed to declare RabbitMQ exchange", "error", err, "exchange_name", TasksExchange)
		return nil, fmt.Errorf("failed to declare RabbitMQ exchange '%s': %w", TasksExchange, err)
	}
	logger.Info("RabbitMQ exchange declared successfully", "exchange_name", TasksExchange)

	return &RabbitMQClient{
		conn:    conn,
		channel: ch,
		logger:  logger,
	}, nil
}

func (c *RabbitMQClient) Publish(ctx context.Context, exchange, routingKey string, body interface{}) error {
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

	// Добавим таймаут для публикации
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second) // Таймаут для операции публикации
	defer cancel()

	err = c.channel.PublishWithContext(publishCtx,
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

type ConsumeOpts struct {
	QueueName    string
	ExchangeName string
	RoutingKey   string
	ConsumerTag  string
}

func (c *RabbitMQClient) Consume(opts ConsumeOpts, handler func(delivery amqp091.Delivery) error) error {
	if c.channel == nil || c.conn.IsClosed() { // Проверка, что канал и соединение активны
		err := fmt.Errorf("cannot consume, RabbitMQ channel or connection is not active")
		c.logger.Error("Pre-consume check failed", "error", err, "queue_name", opts.QueueName)
		return err
	}

	q, err := c.channel.QueueDeclare(
		opts.QueueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		c.logger.Error("Failed to declare a queue for consuming", "error", err, "queue_name", opts.QueueName)
		return fmt.Errorf("failed to declare queue '%s': %w", opts.QueueName, err)
	}
	c.logger.Info("Queue declared for consuming", "queue_name", q.Name, "messages", q.Messages, "consumers", q.Consumers)

	err = c.channel.QueueBind(
		q.Name,
		opts.RoutingKey,
		opts.ExchangeName,
		false,
		nil,
	)
	if err != nil {
		c.logger.Error("Failed to bind a queue for consuming", "error", err, "queue_name", q.Name, "exchange", opts.ExchangeName, "routing_key", opts.RoutingKey)
		return fmt.Errorf("failed to bind queue '%s' to exchange '%s' with key '%s': %w", q.Name, opts.ExchangeName, opts.RoutingKey, err)
	}
	c.logger.Info("Queue bound for consuming", "queue_name", q.Name, "exchange", opts.ExchangeName, "routing_key", opts.RoutingKey)

	// Устанавливаем Quality of Service (QoS) - предвыборка одного сообщения за раз.
	// Это важно, чтобы один воркер не "захватил" все сообщения из очереди, если их много,
	// и другие воркеры (если они есть) могли бы их обработать.
	// Также это помогает с равномерным распределением нагрузки.
	if err := c.channel.Qos(
		1,     // prefetchCount: не более 1 необработанного сообщения на консьюмера
		0,     // prefetchSize: 0 означает без ограничений по размеру
		false, // global: false означает, что QoS применяется к этому каналу, а не ко всем консьюмерам на соединении
	); err != nil {
		c.logger.Error("Failed to set QoS for consumer", "error", err, "queue_name", q.Name)
		return fmt.Errorf("failed to set QoS for queue '%s': %w", q.Name, err)
	}
	c.logger.Info("QoS set for consumer", "queue_name", q.Name, "prefetch_count", 1)

	msgs, err := c.channel.Consume(
		q.Name,
		opts.ConsumerTag,
		false, // auto-ack (ручное подтверждение)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		c.logger.Error("Failed to register a consumer", "error", err, "queue_name", q.Name)
		return fmt.Errorf("failed to register consumer for queue '%s': %w", q.Name, err)
	}

	c.logger.Info("Consumer registered, waiting for messages...", "queue_name", q.Name, "consumer_tag", opts.ConsumerTag)

	// Канал для graceful shutdown этого конкретного консьюмера не нужен,
	// так как горутина завершится, когда закроется канал `msgs`.
	// Канал `forever` в предыдущей версии был немного избыточен.

	for d := range msgs { // Этот цикл будет работать, пока канал msgs открыт
		c.logger.Debug("Received a message", "delivery_tag", d.DeliveryTag, "body_length", len(d.Body), "consumer_tag", opts.ConsumerTag)
		processErr := handler(d)
		if processErr != nil {
			c.logger.Error("Error processing message, sending Nack (requeue=false)", "error", processErr, "delivery_tag", d.DeliveryTag, "consumer_tag", opts.ConsumerTag)
			// Nack - false (не requeue), чтобы "отравленные" сообщения не попадали обратно в очередь бесконечно.
			// В реальной системе здесь бы работала Dead Letter Exchange (DLX).
			if err := d.Nack(false, false); err != nil {
				c.logger.Error("Failed to Nack message", "error", err, "delivery_tag", d.DeliveryTag)
			}
		} else {
			c.logger.Debug("Message processed successfully, sending Ack", "delivery_tag", d.DeliveryTag, "consumer_tag", opts.ConsumerTag)
			if err := d.Ack(false); err != nil { // Ack - false означает, что подтверждаем только это сообщение
				c.logger.Error("Failed to Ack message", "error", err, "delivery_tag", d.DeliveryTag)
			}
		}
	}

	// Сюда мы попадем, если канал `msgs` был закрыт (например, из-за закрытия соединения RabbitMQ)
	c.logger.Info("RabbitMQ message channel closed, consumer stopping", "consumer_tag", opts.ConsumerTag, "queue_name", opts.QueueName)
	return nil // Нормальное завершение консьюмера
}

func (c *RabbitMQClient) Close() {
	if c.channel != nil {
		// Попытка закрыть канал, если он еще не закрыт
		// Ошибка здесь не критична, если соединение все равно закрывается
		_ = c.channel.Close()
		c.logger.Info("RabbitMQ channel closed or attempted to close.")
	}
	if c.conn != nil && !c.conn.IsClosed() {
		// Попытка закрыть соединение
		_ = c.conn.Close()
		c.logger.Info("RabbitMQ connection closed or attempted to close.")
	}
}
