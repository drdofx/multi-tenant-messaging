// @title Multi-Tenant Messaging System API
// @version 1.0
// @description A multi-tenant messaging system with RabbitMQ and PostgreSQL
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.email support@example.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /api/v1

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"multi-tenant-messaging/internal/config"
	"multi-tenant-messaging/internal/handler"
	"multi-tenant-messaging/internal/handler/middleware"
	"multi-tenant-messaging/internal/rabbitmq"
	"multi-tenant-messaging/internal/service"
	"multi-tenant-messaging/internal/tenant"
)

func main() {
	// Load environment variables
	_ = godotenv.Load()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize database
	db, err := sql.Open("pgx", cfg.Database.URL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	// Initialize RabbitMQ connection
	rmqConn := rabbitmq.NewConnection(cfg.RabbitMQ.URL)
	if err := rmqConn.Connect(); err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rmqConn.Close()
	log.Println("Connected to RabbitMQ")

	// Initialize tenant manager
	tenantManager := tenant.NewManager(
		rmqConn.GetConnection(),
		db,
		cfg.Workers.Default,
		cfg.Workers.Max,
		cfg.Workers.MaxRetry,
		cfg.Workers.MessageTTL,
	)

	// Initialize services
	tenantService := service.NewTenantService(db)
	messageService := service.NewMessageService(db)

	// Initialize handlers
	tenantHandler := handler.NewTenantHandler(tenantService)
	messageHandler := handler.NewMessageHandler(messageService)

	// Setup JWT middleware
	jwtMiddleware := middleware.NewJWTMiddleware(cfg.JWT.Secret)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName: "Multi-Tenant Messaging System",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			message := "Internal Server Error"

			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				message = e.Message
			}

			return c.Status(code).JSON(fiber.Map{
				"error": message,
			})
		},
	})

	// Middleware
	app.Use(recover.New())
	app.Use(logger.New())

	// Routes
	api := app.Group("/api/v1")

	// Public routes
	api.Post("/auth/login", func(c *fiber.Ctx) error {
		// Simple login for testing - generates token for any credentials
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			TenantID string `json:"tenant_id"`
		}

		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		// For testing: accept any username/password
		token, err := middleware.GenerateToken(cfg.JWT.Secret, req.TenantID, "admin", cfg.JWT.Expiry)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to generate token",
			})
		}

		return c.JSON(fiber.Map{
			"token": token,
			"type":  "Bearer",
		})
	})

	// Protected routes
	protected := api.Group("/")
	protected.Use(jwtMiddleware)

	// Tenant routes
	tenants := protected.Group("/tenants")
	tenants.Post("/", tenantHandler.CreateTenant)
	tenants.Get("/", tenantHandler.ListTenants)
	tenants.Get("/:id", tenantHandler.GetTenant)
	tenants.Delete("/:id", func(c *fiber.Ctx) error {
		// Call service delete
		err := tenantService.DeleteTenant(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}

		// Stop consumer
		if err := tenantManager.StopConsumer(c.Params("id")); err != nil {
			log.Printf("Warning: failed to stop consumer: %v", err)
		}

		return c.SendStatus(fiber.StatusNoContent)
	})
	tenants.Put("/:id/config/concurrency", func(c *fiber.Ctx) error {
		id := c.Params("id")

		var req service.UpdateConcurrencyRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		// Update in database
		tenant, err := tenantService.UpdateConcurrency(c.Context(), id, req.Workers)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tenant not found",
			})
		}

		// Update worker pool
		if err := tenantManager.UpdateConcurrency(id, req.Workers); err != nil {
			log.Printf("Warning: failed to update concurrency: %v", err)
		}

		return c.JSON(tenant)
	})

	// Message routes
	messages := protected.Group("/messages")
	messages.Post("/", messageHandler.CreateMessage)
	messages.Get("/", messageHandler.ListMessages)
	messages.Get("/:id", messageHandler.GetMessage)

	// Create tenant endpoint with consumer spawning
	tenants.Post("/", func(c *fiber.Ctx) error {
		// Create in database first
		var req service.CreateTenantRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		tenant, err := tenantService.CreateTenant(c.Context(), req)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Spawn consumer
		if err := tenantManager.SpawnConsumer(tenant.ID, tenant.Workers); err != nil {
			log.Printf("Warning: failed to spawn consumer: %v", err)
		}

		return c.Status(fiber.StatusCreated).JSON(tenant)
	})

	// Health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "healthy",
		})
	})

	// Start server in goroutine
	go func() {
		port := cfg.App.Port
		if port == "" {
			port = "8080"
		}
		log.Printf("Server starting on port %s", port)
		if err := app.Listen(fmt.Sprintf(":%s", port)); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	if err := tenantManager.ShutdownAll(); err != nil {
		log.Printf("Error during tenant manager shutdown: %v", err)
	}

	if err := app.Shutdown(); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Server stopped")
}
