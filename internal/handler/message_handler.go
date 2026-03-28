package handler

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"multi-tenant-messaging/internal/service"
)

// MessageHandler handles message-related HTTP requests
type MessageHandler struct {
	service *service.MessageService
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(svc *service.MessageService) *MessageHandler {
	return &MessageHandler{
		service: svc,
	}
}

// CreateMessage godoc
// @Summary Publish a message
// @Description Publish a message to a tenant's queue
// @Tags messages
// @Accept json
// @Produce json
// @Param request body service.CreateMessageRequest true "Message creation request"
// @Success 201 {object} service.MessageResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /messages [post]
func (h *MessageHandler) CreateMessage(c *fiber.Ctx) error {
	var req service.CreateMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	// Validate tenant_id
	if _, err := uuid.Parse(req.TenantID); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid tenant_id",
		})
	}

	// Validate payload
	if req.Payload == nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "payload is required",
		})
	}

	message, err := h.service.CreateMessage(c.Context(), req)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(http.StatusCreated).JSON(message)
}

// ListMessages godoc
// @Summary List messages
// @Description Get messages with cursor pagination
// @Tags messages
// @Produce json
// @Param cursor query string false "Cursor for pagination"
// @Param limit query int false "Limit (default 20, max 100)"
// @Param tenant_id query string false "Filter by tenant"
// @Success 200 {object} service.CursorPaginationResponse
// @Router /messages [get]
func (h *MessageHandler) ListMessages(c *fiber.Ctx) error {
	cursor := c.Query("cursor", "")
	limit := c.QueryInt("limit", 20)
	if limit > 100 {
		limit = 100
	}
	if limit < 1 {
		limit = 1
	}

	tenantID := c.Query("tenant_id", "")

	resp, err := h.service.ListMessages(c.Context(), tenantID, cursor, limit)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// cursorData represents the cursor structure
type cursorData struct {
	C time.Time `json:"c"` // created_at
	I string    `json:"i"` // id
}

// encodeCursor encodes cursor data to base64 string
func encodeCursor(createdAt time.Time, id string) string {
	data := cursorData{C: createdAt, I: id}
	jsonBytes, _ := json.Marshal(data)
	return base64.URLEncoding.EncodeToString(jsonBytes)
}

// decodeCursor decodes base64 cursor string
func decodeCursor(cursor string) (*cursorData, error) {
	if cursor == "" {
		return nil, nil
	}

	jsonBytes, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}

	var data cursorData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, err
	}

	return &data, nil
}

// GetMessage godoc
// @Summary Get message details
// @Description Get details of a specific message
// @Tags messages
// @Produce json
// @Param id path string true "Message ID"
// @Param tenant_id query string true "Tenant ID"
// @Success 200 {object} service.MessageResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /messages/{id} [get]
func (h *MessageHandler) GetMessage(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid message id",
		})
	}

	tenantID := c.Query("tenant_id")
	if tenantID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "tenant_id is required",
		})
	}

	if _, err := uuid.Parse(tenantID); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid tenant_id",
		})
	}

	message, err := h.service.GetMessage(c.Context(), id, tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "message not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(message)
}

// RegisterRoutes registers message routes
func (h *MessageHandler) RegisterRoutes(app *fiber.App, jwtMiddleware fiber.Handler) {
	router := app.Group("/messages")
	router.Use(jwtMiddleware)

	router.Post("/", h.CreateMessage)
	router.Get("/", h.ListMessages)
	router.Get("/:id", h.GetMessage)
}
