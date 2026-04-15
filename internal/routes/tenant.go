package routes

import (
	"github.com/gofiber/fiber/v2"

	"multi-tenant-messaging/internal/handler"
	"multi-tenant-messaging/internal/service"
	tenantpkg "multi-tenant-messaging/internal/tenant"
)

type TenantRoutes struct {
	tenantHandler *handler.TenantHandler
	tenantService *service.TenantService
	tenantManager *tenantpkg.Manager
}

func NewTenantRoutes(
	tenantHandler *handler.TenantHandler,
	tenantService *service.TenantService,
	tenantManager *tenantpkg.Manager,
) *TenantRoutes {
	return &TenantRoutes{
		tenantHandler: tenantHandler,
		tenantService: tenantService,
		tenantManager: tenantManager,
	}
}

func (r *TenantRoutes) Register(app *fiber.App, jwtMiddleware fiber.Handler) {
	api := app.Group("/api/v1")
	protected := api.Group("", jwtMiddleware)

	tenants := protected.Group("/tenants")
	tenants.Post("/", r.createTenant)
	tenants.Get("/", r.tenantHandler.ListTenants)
	tenants.Get("/:id", r.tenantHandler.GetTenant)
	tenants.Delete("/:id", r.deleteTenant)
	tenants.Put("/:id/config/concurrency", r.updateConcurrency)
}

func (r *TenantRoutes) createTenant(c *fiber.Ctx) error {
	var req service.CreateTenantRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	tenant, err := r.tenantService.CreateTenant(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if err := r.tenantManager.SpawnConsumer(tenant.ID, tenant.Workers); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to spawn consumer: " + err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(tenant)
}

func (r *TenantRoutes) deleteTenant(c *fiber.Ctx) error {
	id := c.Params("id")

	err := r.tenantService.DeleteTenant(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "tenant not found",
		})
	}

	if err := r.tenantManager.StopConsumer(id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to stop consumer: " + err.Error(),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (r *TenantRoutes) updateConcurrency(c *fiber.Ctx) error {
	id := c.Params("id")

	var req service.UpdateConcurrencyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	tenant, err := r.tenantService.UpdateConcurrency(c.Context(), id, req.Workers)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if err := r.tenantManager.UpdateConcurrency(id, req.Workers); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to update concurrency: " + err.Error(),
		})
	}

	return c.JSON(tenant)
}
