package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CreateTenantRequest represents a request to create a tenant
type CreateTenantRequest struct {
	Name    string `json:"name"`
	Workers int    `json:"workers,omitempty"`
}

// UpdateConcurrencyRequest represents a request to update tenant concurrency
type UpdateConcurrencyRequest struct {
	Workers int `json:"workers"`
}

// TenantResponse represents a tenant response
type TenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workers   int    `json:"workers"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// TenantService handles tenant business logic
type TenantService struct {
	db *sql.DB
}

// NewTenantService creates a new tenant service
func NewTenantService(db *sql.DB) *TenantService {
	return &TenantService{db: db}
}

// CreateTenant creates a new tenant
func (s *TenantService) CreateTenant(ctx context.Context, req CreateTenantRequest) (*TenantResponse, error) {
	id := uuid.New().String()
	workers := 3
	if req.Workers > 0 && req.Workers <= 50 {
		workers = req.Workers
	}

	query := `
		INSERT INTO tenants (id, name, concurrency)
		VALUES ($1, $2, $3)
		RETURNING id, name, concurrency, created_at, updated_at
	`

	var tenant TenantResponse
	row := s.db.QueryRowContext(ctx, query, id, req.Name, workers)
	err := row.Scan(&tenant.ID, &tenant.Name, &tenant.Workers, &tenant.CreatedAt, &tenant.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return &tenant, nil
}

// GetTenant gets a tenant by ID
func (s *TenantService) GetTenant(ctx context.Context, id string) (*TenantResponse, error) {
	query := `
		SELECT id, name, concurrency, created_at, updated_at
		FROM tenants
		WHERE id = $1
	`

	var tenant TenantResponse
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&tenant.ID, &tenant.Name, &tenant.Workers, &tenant.CreatedAt, &tenant.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &tenant, nil
}

// DeleteTenant deletes a tenant
func (s *TenantService) DeleteTenant(ctx context.Context, id string) error {
	query := `DELETE FROM tenants WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateConcurrency updates tenant concurrency
func (s *TenantService) UpdateConcurrency(ctx context.Context, id string, workers int) (*TenantResponse, error) {
	if workers <= 0 || workers > 50 {
		return nil, fmt.Errorf("workers must be between 1 and 50")
	}

	query := `
		UPDATE tenants
		SET concurrency = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, concurrency, created_at, updated_at
	`

	var tenant TenantResponse
	err := s.db.QueryRowContext(ctx, query, id, workers).Scan(
		&tenant.ID, &tenant.Name, &tenant.Workers, &tenant.CreatedAt, &tenant.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &tenant, nil
}

// ListTenants lists all tenants
func (s *TenantService) ListTenants(ctx context.Context, limit, offset int) ([]TenantResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT id, name, concurrency, created_at, updated_at
		FROM tenants
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []TenantResponse
	for rows.Next() {
		var t TenantResponse
		if err := rows.Scan(&t.ID, &t.Name, &t.Workers, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}

	return tenants, rows.Err()
}

// CreateMessageRequest represents a request to create a message
type CreateMessageRequest struct {
	TenantID string      `json:"tenant_id"`
	Payload  interface{} `json:"payload"`
}

// MessageResponse represents a message response
type MessageResponse struct {
	ID         string      `json:"id"`
	TenantID   string      `json:"tenant_id"`
	Payload    interface{} `json:"payload"`
	Status     string      `json:"status"`
	RetryCount int         `json:"retry_count"`
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
}

// CursorPaginationResponse represents cursor paginated response
type CursorPaginationResponse struct {
	Data       []MessageResponse `json:"data"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more"`
}

// MessageService handles message business logic
type MessageService struct {
	db *sql.DB
}

// NewMessageService creates a new message service
func NewMessageService(db *sql.DB) *MessageService {
	return &MessageService{db: db}
}

// CreateMessage creates a new message
func (s *MessageService) CreateMessage(ctx context.Context, req CreateMessageRequest) (*MessageResponse, error) {
	id := uuid.New().String()

	// Convert payload to JSON
	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	query := `
		INSERT INTO messages (id, tenant_id, payload, status, retry_count)
		VALUES ($1, $2, $3, 'pending', 0)
		RETURNING id, tenant_id, payload, status, retry_count, created_at, updated_at
	`

	var msg MessageResponse
	var payloadBytes []byte
	err = s.db.QueryRowContext(ctx, query, id, req.TenantID, payloadJSON).Scan(
		&msg.ID, &msg.TenantID, &payloadBytes, &msg.Status, &msg.RetryCount, &msg.CreatedAt, &msg.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	// Unmarshal payload for response
	json.Unmarshal(payloadBytes, &msg.Payload)

	return &msg, nil
}

// GetMessage gets a message by ID
func (s *MessageService) GetMessage(ctx context.Context, id, tenantID string) (*MessageResponse, error) {
	query := `
		SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
		FROM messages
		WHERE id = $1 AND tenant_id = $2
	`

	var msg MessageResponse
	var payloadBytes []byte
	err := s.db.QueryRowContext(ctx, query, id, tenantID).Scan(
		&msg.ID, &msg.TenantID, &payloadBytes, &msg.Status, &msg.RetryCount, &msg.CreatedAt, &msg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	json.Unmarshal(payloadBytes, &msg.Payload)
	return &msg, nil
}

// cursorData represents the cursor structure
type cursorData struct {
	C time.Time `json:"c"` // created_at
	I string    `json:"i"` // id
}

// ListMessages lists messages with cursor pagination
func (s *MessageService) ListMessages(ctx context.Context, tenantID, cursor string, limit int) (*CursorPaginationResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Parse cursor
	var cursorTime *time.Time
	
	if cursor != "" {
		// Simple cursor format: base64 encoded time|id
		// For now, simplified - just use time
		t, err := time.Parse(time.RFC3339, cursor)
		if err == nil {
			cursorTime = &t
		}
	}

	// Build query
	var query string
	var args []interface{}

	if tenantID != "" {
		if cursorTime != nil {
			query = `
				SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
				FROM messages
				WHERE tenant_id = $1 AND created_at < $2
				ORDER BY created_at DESC, id DESC
				LIMIT $3
			`
			args = []interface{}{tenantID, *cursorTime, limit + 1}
		} else {
			query = `
				SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
				FROM messages
				WHERE tenant_id = $1
				ORDER BY created_at DESC, id DESC
				LIMIT $2
			`
			args = []interface{}{tenantID, limit + 1}
		}
	} else {
		if cursorTime != nil {
			query = `
				SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
				FROM messages
				WHERE created_at < $1
				ORDER BY created_at DESC, id DESC
				LIMIT $2
			`
			args = []interface{}{*cursorTime, limit + 1}
		} else {
			query = `
				SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
				FROM messages
				ORDER BY created_at DESC, id DESC
				LIMIT $1
			`
			args = []interface{}{limit + 1}
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageResponse
	for rows.Next() {
		var msg MessageResponse
		var payloadBytes []byte
		if err := rows.Scan(&msg.ID, &msg.TenantID, &payloadBytes, &msg.Status, &msg.RetryCount, &msg.CreatedAt, &msg.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(payloadBytes, &msg.Payload)
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Check if there's more data
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	// Generate next cursor
	var nextCursor string
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		lastTime, _ := time.Parse(time.RFC3339, lastMsg.CreatedAt)
		nextCursor = lastTime.Format(time.RFC3339)
	}

	return &CursorPaginationResponse{
		Data:       messages,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
