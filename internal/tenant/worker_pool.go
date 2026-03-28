package tenant

import (
	"sync"
	"sync/atomic"
	"time"
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
	db       interface{}
	maxRetry int
	mu       sync.RWMutex
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(tenantID string, workers int, db interface{}, maxRetry int) *WorkerPool {
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
	} else if diff < 0 {
		// Signal workers to stop (simplified - just update count)
		atomic.StoreInt32(&wp.workers, int32(newCount))
	}
}

// Submit adds a work item to the pool
func (wp *WorkerPool) Submit(item WorkItem) error {
	select {
	case wp.workChan <- item:
		return nil
	case <-wp.doneChan:
		return nil
	default:
		return nil
	}
}

// Stop gracefully shuts down the worker pool
func (wp *WorkerPool) Stop() {
	close(wp.doneChan)
	wp.wg.Wait()
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
		case <-wp.workChan:
			// Process work item (simplified)
			time.Sleep(10 * time.Millisecond)
		}
	}
}
