package rabbitmq

import (
	"encoding/json"
	"fmt"

	"github.com/streadway/amqp"
)

// Message represents a message to be published
type Message struct {
	ID        string      `json:"id"`
	TenantID  string      `json:"tenant_id"`
	Payload   interface{} `json:"payload"`
	Timestamp int64       `json:"timestamp"`
}

// Publisher handles message publishing
type Publisher struct {
	conn *Connection
}

// NewPublisher creates a new message publisher
func NewPublisher(conn *Connection) *Publisher {
	return &Publisher{
		conn: conn,
	}
}

// Publish sends a message to a tenant's queue
func (p *Publisher) Publish(tenantID string, message Message) error {
	ch, err := p.conn.NewChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer ch.Close()

	queueName := fmt.Sprintf("tenant_%s_queue", tenantID)

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	err = ch.Publish(
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
			Headers: amqp.Table{
				"x-tenant-id":  tenantID,
				"x-message-id": message.ID,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// PublishBatch publishes multiple messages (for high throughput)
func (p *Publisher) PublishBatch(tenantID string, messages []Message) error {
	ch, err := p.conn.NewChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer ch.Close()

	queueName := fmt.Sprintf("tenant_%s_queue", tenantID)

	// Use transactions for batch publishing
	if err := ch.Tx(); err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	for _, message := range messages {
		body, err := json.Marshal(message)
		if err != nil {
			ch.TxRollback()
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		err = ch.Publish(
			"",
			queueName,
			false,
			false,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        body,
				Headers: amqp.Table{
					"x-tenant-id":  tenantID,
					"x-message-id": message.ID,
				},
			},
		)
		if err != nil {
			ch.TxRollback()
			return fmt.Errorf("failed to publish message: %w", err)
		}
	}

	if err := ch.TxCommit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
