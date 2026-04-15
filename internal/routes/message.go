package routes

import (
	"github.com/gofiber/fiber/v2"

	"multi-tenant-messaging/internal/handler"
)

type MessageRoutes struct {
	messageHandler *handler.MessageHandler
}

func NewMessageRoutes(messageHandler *handler.MessageHandler) *MessageRoutes {
	return &MessageRoutes{messageHandler: messageHandler}
}

func (r *MessageRoutes) Register(app *fiber.App, jwtMiddleware fiber.Handler) {
	api := app.Group("/api/v1")
	protected := api.Group("", jwtMiddleware)

	messages := protected.Group("/messages")
	messages.Post("/", r.messageHandler.CreateMessage)
	messages.Get("/", r.messageHandler.ListMessages)
	messages.Get("/:id", r.messageHandler.GetMessage)
}
