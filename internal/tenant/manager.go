package tenant

import (
	
	"database/sql"
	
	"fmt"
	"log"
	"sync"
	
	"time"

	"github.com/streadway/amqp"
)

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
	rmConn         interface{}
	db             *sql.DB
	defaultWorkers int
	maxWorkers     int
	maxRetry       int
	messageTTL     time.Duration
}

// NewManager creates a new tenant manager
func NewManager(rmConn interface{}, db *sql.DB, defaultWorkers, maxWorkers, maxRetry int, messageTTL time.Duration) *Manager {
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

	// Create channel (interface type assertion needed for actual implementation)
	// This is simplified - actual implementation would use proper RabbitMQ connection
	consumer := &TenantConsumer{
		TenantID:  tenantID,
		QueueName: fmt.Sprintf("tenant_%s_queue", tenantID),
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
	errChan := make(chan error, len(consumers))

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
	close(errChan)

	log.Printf("Shutdown all consumers")
	return nil
}

// consumeMessages consumes messages from the queue (placeholder implementation)
func (m *Manager) consumeMessages(consumer *TenantConsumer) {
	// This is a placeholder - actual implementation would:
	// 1. Get channel from RabbitMQ connection
	// 2. Call channel.Consume()
	// 3. Route messages to worker pool

	log.Printf("Consumer for tenant %s started", consumer.TenantID)

	<-consumer.Done

	log.Printf("Consumer for tenant %s stopped", consumer.TenantID)
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
