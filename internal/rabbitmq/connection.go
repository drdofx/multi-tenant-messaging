package rabbitmq

import (
	"fmt"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

// Connection wraps amqp.Connection with reconnection logic
type Connection struct {
	url       string
	conn      *amqp.Connection
	channel   *amqp.Channel
	mu        sync.RWMutex
	closed    bool
	reconnect chan bool
}

// NewConnection creates a new RabbitMQ connection manager
func NewConnection(url string) *Connection {
	return &Connection{
		url:       url,
		reconnect: make(chan bool, 1),
	}
}

// Connect establishes connection to RabbitMQ
func (c *Connection) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("connection is closed")
	}

	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	c.conn = conn
	c.channel = ch

	// Setup connection close handler
	go c.handleReconnect()

	return nil
}

// Channel returns the current channel (creates new if needed)
func (c *Connection) Channel() (*amqp.Channel, error) {
	c.mu.RLock()
	if c.channel != nil {
		ch := c.channel
		c.mu.RUnlock()
		return ch, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil || c.conn.IsClosed() {
		if err := c.Connect(); err != nil {
			return nil, err
		}
	}

	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to create channel: %w", err)
	}

	c.channel = ch
	return ch, nil
}

// NewChannel creates a fresh channel
func (c *Connection) NewChannel() (*amqp.Channel, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		if err := c.Connect(); err != nil {
			return nil, err
		}
		c.mu.RLock()
		conn = c.conn
		c.mu.RUnlock()
	}

	return conn.Channel()
}

// Close closes the connection
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	close(c.reconnect)

	if c.channel != nil {
		c.channel.Close()
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// IsClosed returns true if connection is closed
func (c *Connection) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// GetConnection returns the underlying amqp.Connection
func (c *Connection) GetConnection() *amqp.Connection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// handleReconnect handles connection reconnection
func (c *Connection) handleReconnect() {
	closeChan := make(chan *amqp.Error)

	c.mu.RLock()
	if c.conn != nil {
		c.conn.NotifyClose(closeChan)
	}
	c.mu.RUnlock()

	for {
		select {
		case err := <-closeChan:
			if err != nil {
				fmt.Printf("RabbitMQ connection closed: %v, reconnecting...\n", err)
				time.Sleep(5 * time.Second)
				c.Connect()
				return
			}
		case <-c.reconnect:
			return
		}
	}
}

// DeclareTenantQueue creates queues for a tenant including DLX/DLQ
func DeclareTenantQueue(ch *amqp.Channel, tenantID string, ttl time.Duration) (string, string, error) {
	mainQueue := fmt.Sprintf("tenant_%s_queue", tenantID)
	exchangeName := fmt.Sprintf("tenant_%s_dlx", tenantID)
	dlQueue := fmt.Sprintf("tenant_%s_dlq", tenantID)

	// Declare dead letter exchange
	err := ch.ExchangeDeclare(
		exchangeName,
		"fanout",
		true,  // durable
		false, // auto-delete
		false, // internal
		false, // no-wait
		nil,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to declare DLX: %w", err)
	}

	// Declare dead letter queue
	_, err = ch.QueueDeclare(
		dlQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to declare DLQ: %w", err)
	}

	// Bind DLQ to DLX
	err = ch.QueueBind(
		dlQueue,
		"",
		exchangeName,
		false,
		nil,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to bind DLQ: %w", err)
	}

	// Declare main queue with DLX
	args := amqp.Table{
		"x-dead-letter-exchange": exchangeName,
	}
	if ttl > 0 {
		args["x-message-ttl"] = int32(ttl.Milliseconds())
	}

	_, err = ch.QueueDeclare(
		mainQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		args,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to declare main queue: %w", err)
	}

	return mainQueue, dlQueue, nil
}

// DeleteTenantQueue removes all tenant queues and exchanges
func DeleteTenantQueue(ch *amqp.Channel, tenantID string) error {
	mainQueue := fmt.Sprintf("tenant_%s_queue", tenantID)
	exchangeName := fmt.Sprintf("tenant_%s_dlx", tenantID)
	dlQueue := fmt.Sprintf("tenant_%s_dlq", tenantID)

	// Delete main queue
	_, err := ch.QueueDelete(mainQueue, false, false, false)
	if err != nil {
		return fmt.Errorf("failed to delete main queue: %w", err)
	}

	// Delete DLQ
	_, err = ch.QueueDelete(dlQueue, false, false, false)
	if err != nil {
		return fmt.Errorf("failed to delete DLQ: %w", err)
	}

	// Delete DLX
	err = ch.ExchangeDelete(exchangeName, false, false)
	if err != nil {
		return fmt.Errorf("failed to delete DLX: %w", err)
	}

	return nil
}
