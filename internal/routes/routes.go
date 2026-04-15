package routes

import (
	"github.com/gofiber/fiber/v2"

	"multi-tenant-messaging/internal/handler"
	tenantpkg "multi-tenant-messaging/internal/tenant"
	"multi-tenant-messaging/internal/service"
)

type Router struct {
	authHandler    *handler.AuthHandler
	tenantHandler  *handler.TenantHandler
	messageHandler *handler.MessageHandler
	tenantManager  *tenantpkg.Manager
	tenantService  *service.TenantService
	jwtMiddleware  fiber.Handler
}

func NewRouter(
	authHandler *handler.AuthHandler,
	tenantHandler *handler.TenantHandler,
	messageHandler *handler.MessageHandler,
	tenantManager *tenantpkg.Manager,
	tenantService *service.TenantService,
	jwtMiddleware fiber.Handler,
) *Router {
	return &Router{
		authHandler:    authHandler,
		tenantHandler:  tenantHandler,
		messageHandler: messageHandler,
		tenantManager:  tenantManager,
		tenantService:  tenantService,
		jwtMiddleware:  jwtMiddleware,
	}
}

func (r *Router) Register(app *fiber.App) {
	authRoutes := NewAuthRoutes(r.authHandler)
	authRoutes.Register(app)

	tenantRoutes := NewTenantRoutes(r.tenantHandler, r.tenantService, r.tenantManager)
	tenantRoutes.Register(app, r.jwtMiddleware)

	messageRoutes := NewMessageRoutes(r.messageHandler)
	messageRoutes.Register(app, r.jwtMiddleware)

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "healthy",
		})
	})
}
