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
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"

	"multi-tenant-messaging/internal/config"
	"multi-tenant-messaging/internal/handler"
	"multi-tenant-messaging/internal/handler/middleware"
	"multi-tenant-messaging/internal/rabbitmq"
	"multi-tenant-messaging/internal/repository"
	"multi-tenant-messaging/internal/routes"
	"multi-tenant-messaging/internal/service"
	tenantpkg "multi-tenant-messaging/internal/tenant"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not loaded")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	db, err := sql.Open("pgx", cfg.Database.URL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	if err := goose.Up(db, "db/migrations"); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Migrations completed")

	rmqConn := rabbitmq.NewConnection(cfg.RabbitMQ.URL)
	if err := rmqConn.Connect(); err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rmqConn.Close()
	log.Println("Connected to RabbitMQ")

	tenantManager := tenantpkg.NewManager(
		rmqConn.GetConnection(),
		db,
		cfg.Workers.Default,
		cfg.Workers.Max,
		cfg.Workers.MaxRetry,
		cfg.Workers.MessageTTL,
	)

	userRepo := repository.NewUserRepository(db)
	authService := service.NewAuthService(userRepo, cfg.JWT.Secret, cfg.JWT.Expiry)
	tenantService := service.NewTenantService(db)
	messageService := service.NewMessageService(db)

	authHandler := handler.NewAuthHandler(authService)
	tenantHandler := handler.NewTenantHandler(tenantService)
	messageHandler := handler.NewMessageHandler(messageService)

	jwtMiddleware := middleware.NewJWTMiddleware(cfg.JWT.Secret)

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

	app.Use(recover.New())
	app.Use(logger.New())

	router := routes.NewRouter(
		authHandler,
		tenantHandler,
		messageHandler,
		tenantManager,
		tenantService,
		jwtMiddleware,
	)
	router.Register(app)

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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	if err := tenantManager.ShutdownAll(); err != nil {
		log.Printf("Error during tenant manager shutdown: %v", err)
	}

	if err := app.Shutdown(); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Server stopped")
}
