package handler

import (
	"database/sql"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"multi-tenant-messaging/internal/service"
)

// TenantHandler handles tenant-related HTTP requests
type TenantHandler struct {
	service *service.TenantService
}

// NewTenantHandler creates a new tenant handler
func NewTenantHandler(svc *service.TenantService) *TenantHandler {
	return &TenantHandler{
		service: svc,
	}
}

// CreateTenant godoc
// @Summary Create a new tenant
// @Description Create a new tenant and spawn a message consumer
// @Tags tenants
// @Accept json
// @Produce json
// @Param request body service.CreateTenantRequest true "Tenant creation request"
// @Success 201 {object} service.TenantResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tenants [post]
func (h *TenantHandler) CreateTenant(c *fiber.Ctx) error {
	var req service.CreateTenantRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	// Validate
	if req.Name == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "name is required",
		})
	}

	tenant, err := h.service.CreateTenant(c.Context(), req)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(http.StatusCreated).JSON(tenant)
}

// DeleteTenant godoc
// @Summary Delete a tenant
// @Description Delete a tenant and stop its consumer
// @Tags tenants
// @Produce json
// @Param id path string true "Tenant ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tenants/{id} [delete]
func (h *TenantHandler) DeleteTenant(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid tenant id",
		})
	}

	if err := h.service.DeleteTenant(c.Context(), id); err != nil {
		if err == sql.ErrNoRows {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.SendStatus(http.StatusNoContent)
}

// GetTenant godoc
// @Summary Get tenant details
// @Description Get details of a specific tenant
// @Tags tenants
// @Produce json
// @Param id path string true "Tenant ID"
// @Success 200 {object} service.TenantResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /tenants/{id} [get]
func (h *TenantHandler) GetTenant(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid tenant id",
		})
	}

	tenant, err := h.service.GetTenant(c.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(tenant)
}

// UpdateConcurrency godoc
// @Summary Update tenant concurrency
// @Description Update the number of workers for a tenant
// @Tags tenants
// @Accept json
// @Produce json
// @Param id path string true "Tenant ID"
// @Param request body service.UpdateConcurrencyRequest true "Concurrency update request"
// @Success 200 {object} service.TenantResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tenants/{id}/config/concurrency [put]
func (h *TenantHandler) UpdateConcurrency(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid tenant id",
		})
	}

	var req service.UpdateConcurrencyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.Workers <= 0 || req.Workers > 50 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "workers must be between 1 and 50",
		})
	}

	tenant, err := h.service.UpdateConcurrency(c.Context(), id, req.Workers)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(tenant)
}

// ListTenants godoc
// @Summary List all tenants
// @Description Get a list of all tenants
// @Tags tenants
// @Produce json
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Success 200 {array} service.TenantResponse
// @Router /tenants [get]
func (h *TenantHandler) ListTenants(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	tenants, err := h.service.ListTenants(c.Context(), limit, offset)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(tenants)
}

// RegisterRoutes registers tenant routes
func (h *TenantHandler) RegisterRoutes(app *fiber.App, jwtMiddleware fiber.Handler) {
	router := app.Group("/tenants")
	router.Use(jwtMiddleware)

	router.Post("/", h.CreateTenant)
	router.Get("/", h.ListTenants)
	router.Get("/:id", h.GetTenant)
	router.Delete("/:id", h.DeleteTenant)
	router.Put("/:id/config/concurrency", h.UpdateConcurrency)
}
