package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/streadway/amqp"
)

// WorkItem represents a unit of work to be processed
type WorkItem struct {
	MessageID  string
	TenantID   string
	Payload    []byte
	RetryCount int
}

// WorkerPool manages a pool of workers for processing messages
type WorkerPool struct {
	tenantID string
	workers  int32
	workChan chan WorkItem
	doneChan chan struct{}
	wg       sync.WaitGroup
	db       *sql.DB
	maxRetry int
	mu       sync.RWMutex
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(tenantID string, workers int, db *sql.DB, maxRetry int) *WorkerPool {
	wp := &WorkerPool{
		tenantID: tenantID,
		workChan: make(chan WorkItem, workers*10),
		doneChan: make(chan struct{}),
		db:       db,
		maxRetry: maxRetry,
	}

	wp.Start(workers)
	return wp
}

// Start starts the worker pool with the specified number of workers
func (wp *WorkerPool) Start(workers int) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if workers <= 0 {
		workers = 1
	}

	atomic.StoreInt32(&wp.workers, int32(workers))

	for i := 0; i < workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// Resize dynamically resizes the worker pool
func (wp *WorkerPool) Resize(newCount int) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if newCount <= 0 {
		newCount = 1
	}

	currentCount := atomic.LoadInt32(&wp.workers)
	diff := newCount - int(currentCount)

	if diff > 0 {
		// Add workers
		for i := 0; i < diff; i++ {
			wp.wg.Add(1)
			go wp.worker()
		}
		atomic.AddInt32(&wp.workers, int32(diff))
		log.Printf("Worker pool for tenant %s scaled up to %d workers", wp.tenantID, newCount)
	} else if diff < 0 {
		// Remove workers by signaling
		atomic.StoreInt32(&wp.workers, int32(newCount))
		log.Printf("Worker pool for tenant %s scaled down to %d workers", wp.tenantID, newCount)
	}
}

// Submit adds a work item to the pool
func (wp *WorkerPool) Submit(item WorkItem) error {
	select {
	case wp.workChan <- item:
		return nil
	case <-wp.doneChan:
		return fmt.Errorf("worker pool is shutting down")
	default:
		return fmt.Errorf("worker pool queue is full")
	}
}

// Stop gracefully shuts down the worker pool
func (wp *WorkerPool) Stop() {
	close(wp.doneChan)
	wp.wg.Wait()
	log.Printf("Worker pool for tenant %s stopped", wp.tenantID)
}

// GetWorkerCount returns the current number of workers
func (wp *WorkerPool) GetWorkerCount() int {
	return int(atomic.LoadInt32(&wp.workers))
}

// worker is the goroutine that processes work items
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.doneChan:
			return
		case item := <-wp.workChan:
			wp.processWork(item)
		}
	}
}

// processWork processes a single work item with retry logic
func (wp *WorkerPool) processWork(item WorkItem) {
	// Process with exponential backoff retry
	err := wp.processWithRetry(item)

	if err != nil {
		log.Printf("Failed to process message %s after %d retries: %v", item.MessageID, item.RetryCount, err)

		// Max retry reached, move to DLQ
		if item.RetryCount >= wp.maxRetry {
			wp.moveToDeadLetter(item, err)
		}
	}
}

// processWithRetry processes message with exponential backoff
func (wp *WorkerPool) processWithRetry(item WorkItem) error {
	for attempt := 0; attempt <= wp.maxRetry; attempt++ {
		// Try to process
		err := wp.handleMessage(item)
		if err == nil {
			return nil // Success!
		}

		// Failed, calculate exponential backoff
		item.RetryCount = attempt + 1

		if item.RetryCount > wp.maxRetry {
			return err // Max retries exceeded
		}

		// Exponential backoff: 2^attempt seconds (1s, 2s, 4s, 8s...)
		backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * time.Second

		// Add some jitter to prevent thundering herd (±20%)
		jitter := time.Duration(float64(backoffDuration) * 0.2 * (float64(time.Now().UnixNano()%100) / 100.0))
		backoffDuration = backoffDuration + jitter

		log.Printf("Message %s failed (attempt %d/%d), retrying in %v: %v",
			item.MessageID, item.RetryCount, wp.maxRetry, backoffDuration, err)

		time.Sleep(backoffDuration)
	}

	return fmt.Errorf("max retries exceeded")
}

// handleMessage processes the actual message
func (wp *WorkerPool) handleMessage(item WorkItem) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse payload
	var payload map[string]interface{}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Insert or update message in database
	query := `
		INSERT INTO messages (id, tenant_id, payload, status, retry_count, updated_at)
		VALUES ($1, $2, $3, 'processing', $4, NOW())
		ON CONFLICT (id, tenant_id) DO UPDATE 
		SET status = 'processing', retry_count = $4, updated_at = NOW()
		WHERE messages.tenant_id = $2
	`

	_, err := wp.db.ExecContext(ctx, query, item.MessageID, item.TenantID, item.Payload, item.RetryCount)
	if err != nil {
		return fmt.Errorf("failed to update message status: %w", err)
	}

	// Simulate message processing
	// In real implementation, this would execute business logic
	// For demo purposes, we'll randomly fail 10% of messages to test retry
	if time.Now().UnixNano()%10 == 0 {
		return fmt.Errorf("simulated processing failure")
	}

	time.Sleep(100 * time.Millisecond) // Simulate work

	// Mark as completed
	_, err = wp.db.ExecContext(ctx,
		`UPDATE messages SET status = 'completed', updated_at = NOW() 
		 WHERE id = $1 AND tenant_id = $2`,
		item.MessageID, item.TenantID)
	if err != nil {
		return fmt.Errorf("failed to mark message as completed: %w", err)
	}

	return nil
}

// moveToDeadLetter moves a failed message to the dead letter queue
func (wp *WorkerPool) moveToDeadLetter(item WorkItem, processErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert into dead_letter_messages
	query := `
		INSERT INTO dead_letter_messages (original_message_id, tenant_id, payload, error_reason, retry_count)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := wp.db.ExecContext(ctx, query, item.MessageID, item.TenantID, item.Payload, processErr.Error(), item.RetryCount)
	if err != nil {
		log.Printf("Failed to move message %s to dead letter queue: %v", item.MessageID, err)
	}

	// Update message status to failed
	wp.db.ExecContext(ctx,
		`UPDATE messages SET status = 'failed', updated_at = NOW() 
		 WHERE id = $1 AND tenant_id = $2`,
		item.MessageID, item.TenantID)

	log.Printf("Message %s moved to dead letter queue after %d retries", item.MessageID, item.RetryCount)
}

// SubmitRabbitMQMessage submits a message from RabbitMQ to the worker pool
func (wp *WorkerPool) SubmitRabbitMQMessage(delivery amqp.Delivery) error {
	item := WorkItem{
		MessageID:  delivery.MessageId,
		TenantID:   wp.tenantID,
		Payload:    delivery.Body,
		RetryCount: 0,
	}

	// Try to extract retry count from headers
	if headers, ok := delivery.Headers["x-retry-count"].(int32); ok {
		item.RetryCount = int(headers)
	}

	return wp.Submit(item)
}
