package routes

import (
	"github.com/gofiber/fiber/v2"

	"multi-tenant-messaging/internal/handler"
)

type AuthRoutes struct {
	authHandler *handler.AuthHandler
}

func NewAuthRoutes(authHandler *handler.AuthHandler) *AuthRoutes {
	return &AuthRoutes{authHandler: authHandler}
}

func (r *AuthRoutes) Register(app *fiber.App) {
	api := app.Group("/api/v1")

	auth := api.Group("/auth")
	auth.Post("/register", r.authHandler.Register)
	auth.Post("/login", r.authHandler.Login)
}
