package tenant

// CreateTenantRequest represents a request to create a tenant
type CreateTenantRequest struct {
	Name    string `json:"name" validate:"required,min=3,max=255"`
	Workers int    `json:"workers,omitempty"`
}

// UpdateConcurrencyRequest represents a request to update tenant concurrency
type UpdateConcurrencyRequest struct {
	Workers int `json:"workers" validate:"required,min=1,max=50"`
}

// TenantResponse represents a tenant response
type TenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workers   int    `json:"workers"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CreateMessageRequest represents a request to create a message
type CreateMessageRequest struct {
	TenantID string      `json:"tenant_id" validate:"required,uuid"`
	Payload  interface{} `json:"payload" validate:"required"`
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

// CursorPaginationRequest represents cursor pagination parameters
type CursorPaginationRequest struct {
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" validate:"min=1,max=100"`
}

// CursorPaginationResponse represents cursor paginated response
type CursorPaginationResponse struct {
	Data       []MessageResponse `json:"data"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token string `json:"token"`
	Type  string `json:"type"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
