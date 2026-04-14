package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

// Message represents a message from RabbitMQ
type Message struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

// TenantConsumer represents a tenant's message consumer
type TenantConsumer struct {
	TenantID   string
	QueueName  string
	WorkerPool *WorkerPool
	Channel    *amqp.Channel
	Done       chan struct{}
}

// Manager manages tenant consumers
type Manager struct {
	consumers      map[string]*TenantConsumer
	mu             sync.RWMutex
	rmConn         *amqp.Connection
	db             *sql.DB
	defaultWorkers int
	maxWorkers     int
	maxRetry       int
	messageTTL     time.Duration
}

// NewManager creates a new tenant manager
func NewManager(rmConn *amqp.Connection, db *sql.DB, defaultWorkers, maxWorkers, maxRetry int, messageTTL time.Duration) *Manager {
	return &Manager{
		consumers:      make(map[string]*TenantConsumer),
		rmConn:         rmConn,
		db:             db,
		defaultWorkers: defaultWorkers,
		maxWorkers:     maxWorkers,
		maxRetry:       maxRetry,
		messageTTL:     messageTTL,
	}
}

// SpawnConsumer creates a new consumer for a tenant
func (m *Manager) SpawnConsumer(tenantID string, concurrency int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if consumer already exists
	if _, exists := m.consumers[tenantID]; exists {
		return fmt.Errorf("consumer already exists for tenant %s", tenantID)
	}

	// Validate concurrency
	if concurrency <= 0 {
		concurrency = m.defaultWorkers
	}
	if concurrency > m.maxWorkers {
		concurrency = m.maxWorkers
	}

	// Create RabbitMQ channel
	ch, err := m.rmConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}

	// Declare queues for tenant
	queueName, _, err := m.declareTenantQueues(ch, tenantID)
	if err != nil {
		ch.Close()
		return fmt.Errorf("failed to declare queues: %w", err)
	}

	// Create consumer
	consumer := &TenantConsumer{
		TenantID:  tenantID,
		QueueName: queueName,
		Channel:   ch,
		Done:      make(chan struct{}),
	}

	// Initialize worker pool
	consumer.WorkerPool = NewWorkerPool(
		tenantID,
		concurrency,
		m.db,
		m.maxRetry,
	)

	m.consumers[tenantID] = consumer

	// Start consuming in goroutine
	go m.consumeMessages(consumer)

	log.Printf("Spawned consumer for tenant %s with %d workers", tenantID, concurrency)
	return nil
}

// StopConsumer stops a tenant's consumer
func (m *Manager) StopConsumer(tenantID string) error {
	m.mu.Lock()
	consumer, exists := m.consumers[tenantID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("consumer not found for tenant %s", tenantID)
	}

	delete(m.consumers, tenantID)
	m.mu.Unlock()

	// Signal shutdown
	close(consumer.Done)

	// Stop worker pool
	if consumer.WorkerPool != nil {
		consumer.WorkerPool.Stop()
	}

	// Close channel
	if consumer.Channel != nil {
		consumer.Channel.Close()
	}

	log.Printf("Stopped consumer for tenant %s", tenantID)
	return nil
}

// UpdateConcurrency updates the number of workers for a tenant
func (m *Manager) UpdateConcurrency(tenantID string, workers int) error {
	m.mu.RLock()
	consumer, exists := m.consumers[tenantID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("consumer not found for tenant %s", tenantID)
	}

	// Validate workers
	if workers <= 0 {
		workers = 1
	}
	if workers > m.maxWorkers {
		workers = m.maxWorkers
	}

	// Resize worker pool
	if consumer.WorkerPool != nil {
		consumer.WorkerPool.Resize(workers)
	}

	log.Printf("Updated concurrency for tenant %s to %d workers", tenantID, workers)
	return nil
}

// GetConsumer returns a tenant's consumer
func (m *Manager) GetConsumer(tenantID string) (*TenantConsumer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	consumer, exists := m.consumers[tenantID]
	return consumer, exists
}

// ShutdownAll stops all consumers
func (m *Manager) ShutdownAll() error {
	m.mu.Lock()
	consumers := make([]*TenantConsumer, 0, len(m.consumers))
	for _, c := range m.consumers {
		consumers = append(consumers, c)
	}
	m.consumers = make(map[string]*TenantConsumer)
	m.mu.Unlock()

	var wg sync.WaitGroup

	for _, consumer := range consumers {
		wg.Add(1)
		go func(c *TenantConsumer) {
			defer wg.Done()

			close(c.Done)

			if c.WorkerPool != nil {
				c.WorkerPool.Stop()
			}

			if c.Channel != nil {
				c.Channel.Close()
			}
		}(consumer)
	}

	wg.Wait()

	log.Printf("Shutdown all consumers")
	return nil
}

// declareTenantQueues creates queues for a tenant including DLX/DLQ
func (m *Manager) declareTenantQueues(ch *amqp.Channel, tenantID string) (string, string, error) {
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
	if m.messageTTL > 0 {
		args["x-message-ttl"] = int32(m.messageTTL.Milliseconds())
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

// consumeMessages consumes messages from RabbitMQ and routes to worker pool
func (m *Manager) consumeMessages(consumer *TenantConsumer) {
	log.Printf("Consumer for tenant %s started", consumer.TenantID)

	// Start consuming
	msgs, err := consumer.Channel.Consume(
		consumer.QueueName, // queue
		"",                 // consumer
		false,              // auto-ack (manual ack for retry)
		false,              // exclusive
		false,              // no-local
		false,              // no-wait
		nil,                // args
	)
	if err != nil {
		log.Printf("Failed to start consuming for tenant %s: %v", consumer.TenantID, err)
		return
	}

	for {
		select {
		case <-consumer.Done:
			log.Printf("Consumer for tenant %s stopping...", consumer.TenantID)
			return
		case msg, ok := <-msgs:
			if !ok {
				log.Printf("Message channel closed for tenant %s", consumer.TenantID)
				return
			}

			// Process message
			if err := m.processMessage(consumer, msg); err != nil {
				log.Printf("Error processing message for tenant %s: %v", consumer.TenantID, err)
			}
		}
	}
}

// processMessage processes a single message from RabbitMQ
func (m *Manager) processMessage(consumer *TenantConsumer, msg amqp.Delivery) error {
	// Parse message
	var message Message
	if err := json.Unmarshal(msg.Body, &message); err != nil {
		// Nack and don't requeue - invalid message
		msg.Nack(false, false)
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Submit to worker pool
	item := WorkItem{
		MessageID:  message.ID,
		TenantID:   consumer.TenantID,
		Payload:    message.Payload,
		RetryCount: 0,
	}

	if err := consumer.WorkerPool.Submit(item); err != nil {
		// Worker pool full or shutting down, requeue message
		msg.Nack(false, true)
		return fmt.Errorf("failed to submit to worker pool: %w", err)
	}

	// Acknowledge message
	if err := msg.Ack(false); err != nil {
		return fmt.Errorf("failed to ack message: %w", err)
	}

	return nil
}

// GetConsumerCount returns the number of active consumers
func (m *Manager) GetConsumerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.consumers)
}

// ListConsumers returns a list of active tenant IDs
func (m *Manager) ListConsumers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.consumers))
	for id := range m.consumers {
		ids = append(ids, id)
	}
	return ids
}

// DeleteTenantQueues deletes all queues for a tenant
func (m *Manager) DeleteTenantQueues(tenantID string) error {
	ch, err := m.rmConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}
	defer ch.Close()

	mainQueue := fmt.Sprintf("tenant_%s_queue", tenantID)
	exchangeName := fmt.Sprintf("tenant_%s_dlx", tenantID)
	dlQueue := fmt.Sprintf("tenant_%s_dlq", tenantID)

	// Delete main queue
	_, err = ch.QueueDelete(mainQueue, false, false, false)
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

// PublishMessage publishes a message to a tenant's queue
func (m *Manager) PublishMessage(ctx context.Context, tenantID string, message Message) error {
	ch, err := m.rmConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
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
			MessageId:   message.ID,
			Headers: amqp.Table{
				"x-tenant-id": tenantID,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}
