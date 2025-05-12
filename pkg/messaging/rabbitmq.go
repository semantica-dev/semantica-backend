// File: pkg/messaging/rabbitmq.go
package messaging

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rabbitmq/amqp091-go"
)

type RabbitMQClient struct {
	conn    *amqp091.Connection
	channel *amqp091.Channel
	logger  *slog.Logger
}

func NewRabbitMQClient(amqpURL string, logger *slog.Logger) (*RabbitMQClient, error) {
	conn, err := amqp091.Dial(amqpURL)
	if err != nil {
		logger.Error("Failed to connect to RabbitMQ", "error", err)
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		logger.Error("Failed to open a channel", "error", err)
		return nil, err
	}

	// Объявляем Exchange, если он еще не существует
	// Сделаем его topic, чтобы можно было гибко настраивать роутинг
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
		logger.Error("Failed to declare an exchange", "error", err, "exchange_name", TasksExchange)
		return nil, err
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
		c.logger.Error("Failed to marshal body to JSON", "error", err)
		return err
	}

	msg := amqp091.Publishing{
		ContentType:  "application/json",
		Body:         jsonBody,
		DeliveryMode: amqp091.Persistent, // Сообщения будут сохраняться при перезапуске RabbitMQ
	}

	err = c.channel.PublishWithContext(ctx,
		exchange,   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		msg,
	)
	if err != nil {
		c.logger.Error("Failed to publish a message", "error", err, "exchange", exchange, "routing_key", routingKey)
		return err
	}
	c.logger.Info("Message published", "exchange", exchange, "routing_key", routingKey, "body_length", len(jsonBody))
	return nil
}

// ConsumeOpts содержит опции для консьюмера
type ConsumeOpts struct {
	QueueName    string
	ExchangeName string
	RoutingKey   string
	ConsumerTag  string // Уникальный тег для консьюмера
}

// Consume начинает прослушивание очереди и вызывает handler для каждого сообщения.
// Handler должен вернуть error, если сообщение не может быть обработано (будет Nack).
// Если handler возвращает nil, сообщение будет Ack'нуто.
func (c *RabbitMQClient) Consume(opts ConsumeOpts, handler func(delivery amqp091.Delivery) error) error {
	q, err := c.channel.QueueDeclare(
		opts.QueueName, // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		c.logger.Error("Failed to declare a queue", "error", err, "queue_name", opts.QueueName)
		return err
	}
	c.logger.Info("Queue declared", "queue_name", q.Name, "messages", q.Messages, "consumers", q.Consumers)

	err = c.channel.QueueBind(
		q.Name,            // queue name
		opts.RoutingKey,   // routing key
		opts.ExchangeName, // exchange
		false,
		nil,
	)
	if err != nil {
		c.logger.Error("Failed to bind a queue", "error", err, "queue_name", q.Name, "exchange", opts.ExchangeName, "routing_key", opts.RoutingKey)
		return err
	}
	c.logger.Info("Queue bound", "queue_name", q.Name, "exchange", opts.ExchangeName, "routing_key", opts.RoutingKey)

	msgs, err := c.channel.Consume(
		q.Name,           // queue
		opts.ConsumerTag, // consumer
		false,            // auto-ack (мы будем делать это вручную)
		false,            // exclusive
		false,            // no-local
		false,            // no-wait
		nil,              // args
	)
	if err != nil {
		c.logger.Error("Failed to register a consumer", "error", err, "queue_name", q.Name)
		return err
	}

	c.logger.Info("Consumer registered, waiting for messages...", "queue_name", q.Name, "consumer_tag", opts.ConsumerTag)

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			c.logger.Debug("Received a message", "delivery_tag", d.DeliveryTag, "body_length", len(d.Body))
			err := handler(d)
			if err != nil {
				c.logger.Error("Error processing message, sending Nack", "error", err, "delivery_tag", d.DeliveryTag)
				// Nack - с requeue=false, чтобы не попасть в бесконечный цикл, если сообщение "отравлено"
				// В реальном приложении можно настроить Dead Letter Exchange (DLX)
				d.Nack(false, false)
			} else {
				c.logger.Debug("Message processed successfully, sending Ack", "delivery_tag", d.DeliveryTag)
				d.Ack(false) // Ack - false означает, что подтверждаем только это сообщение
			}
		}
		c.logger.Info("RabbitMQ channel closed, consumer stopping", "consumer_tag", opts.ConsumerTag)
		forever <- true // Сигнал о завершении, если канал сообщений закрылся
	}()

	<-forever // Блокируемся до тех пор, пока consumer не остановится
	c.logger.Info("Consumer shutdown gracefully", "consumer_tag", opts.ConsumerTag)
	return nil
}

func (c *RabbitMQClient) Close() {
	if c.channel != nil {
		c.channel.Close()
		c.logger.Info("RabbitMQ channel closed")
	}
	if c.conn != nil {
		c.conn.Close()
		c.logger.Info("RabbitMQ connection closed")
	}
}
