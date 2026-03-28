//go:build integration
// +build integration

package test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ory/dockertest/v3"

	"multi-tenant-messaging/internal/handler"
	"multi-tenant-messaging/internal/handler/middleware"
	"multi-tenant-messaging/internal/rabbitmq"
	"multi-tenant-messaging/internal/service"
	"multi-tenant-messaging/internal/tenant"
)

var (
	db        *sql.DB
	rmqConn   *rabbitmq.Connection
	app       *fiber.App
	testToken string
)

func TestMain(m *testing.M) {
	// Setup test environment
	pool, err := dockertest.NewPool("")
	if err != nil {
		panic(fmt.Sprintf("Could not connect to docker: %s", err))
	}

	// Run PostgreSQL
	pgResource, err := pool.Run("postgres", "15", []string{
		"POSTGRES_USER=test",
		"POSTGRES_PASSWORD=test",
		"POSTGRES_DB=test",
	})
	if err != nil {
		panic(fmt.Sprintf("Could not start postgres: %s", err))
	}
	defer pool.Purge(pgResource)

	// Run RabbitMQ
	rmqResource, err := pool.Run("rabbitmq", "3.12-management", nil)
	if err != nil {
		panic(fmt.Sprintf("Could not start rabbitmq: %s", err))
	}
	defer pool.Purge(rmqResource)

	// Wait for services
	pgPort := pgResource.GetPort("5432/tcp")
	rmqPort := rmqResource.GetPort("5672/tcp")

	dbURL := fmt.Sprintf("postgres://test:test@localhost:%s/test?sslmode=disable", pgPort)
	rmqURL := fmt.Sprintf("amqp://guest:guest@localhost:%s/", rmqPort)

	// Connect to database
	if err := pool.Retry(func() error {
		var err error
		db, err = sql.Open("pgx", dbURL)
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		panic(fmt.Sprintf("Could not connect to postgres: %s", err))
	}
	defer db.Close()

	// Run migrations
	if err := runMigrations(db); err != nil {
		panic(fmt.Sprintf("Could not run migrations: %s", err))
	}

	// Connect to RabbitMQ
	if err := pool.Retry(func() error {
		rmqConn = rabbitmq.NewConnection(rmqURL)
		return rmqConn.Connect()
	}); err != nil {
		panic(fmt.Sprintf("Could not connect to rabbitmq: %s", err))
	}
	defer rmqConn.Close()

	// Initialize services and handlers
	tenantService := service.NewTenantService(db)
	messageService := service.NewMessageService(db)
	tenantManager := tenant.NewManager(rmqConn, db, 3, 50, 3, 30*time.Second)

	tenantHandler := handler.NewTenantHandler(tenantService)
	messageHandler := handler.NewMessageHandler(messageService)

	// Generate test token
	testToken, _ = middleware.GenerateToken("test-secret", "test-tenant", "admin", time.Hour)

	// Setup Fiber app
	app = fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.JWTClaims{TenantID: "test-tenant"})
		return c.Next()
	})

	// Routes
	app.Post("/api/v1/tenants", tenantHandler.CreateTenant)
	app.Delete("/api/v1/tenants/:id", func(c *fiber.Ctx) error {
		err := tenantService.DeleteTenant(c.Context(), c.Params("id"))
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		tenantManager.StopConsumer(c.Params("id"))
		return c.SendStatus(204)
	})
	app.Put("/api/v1/tenants/:id/config/concurrency", tenantHandler.UpdateConcurrency)
	app.Get("/api/v1/tenants/:id", tenantHandler.GetTenant)
	app.Get("/api/v1/tenants", tenantHandler.ListTenants)
	app.Post("/api/v1/messages", messageHandler.CreateMessage)
	app.Get("/api/v1/messages", messageHandler.ListMessages)
	app.Get("/api/v1/messages/:id", messageHandler.GetMessage)

	// Run tests
	code := m.Run()
	os.Exit(code)
}

func runMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS tenants (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(255) NOT NULL UNIQUE,
			concurrency INT DEFAULT 3,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL,
			payload JSONB NOT NULL,
			status VARCHAR(50) DEFAULT 'pending',
			retry_count INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			CONSTRAINT fk_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
		) PARTITION BY LIST (tenant_id)`,
		`CREATE TABLE IF NOT EXISTS dead_letter_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			original_message_id UUID NOT NULL,
			tenant_id UUID NOT NULL,
			payload JSONB NOT NULL,
			error_reason TEXT,
			retry_count INT DEFAULT 0,
			failed_at TIMESTAMPTZ DEFAULT NOW()
		)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return err
		}
	}
	return nil
}

func TestTenantLifecycle(t *testing.T) {
	// Create tenant
	reqBody := map[string]interface{}{
		"name":    "test-tenant",
		"workers": 3,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/tenants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	var tenant map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tenant)
	tenantID := tenant["id"].(string)

	// Get tenant
	req, _ = http.NewRequest("GET", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Update concurrency
	updateBody := map[string]interface{}{"workers": 5}
	body, _ = json.Marshal(updateBody)
	req, _ = http.NewRequest("PUT", fmt.Sprintf("/api/v1/tenants/%s/config/concurrency", tenantID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Delete tenant
	req, _ = http.NewRequest("DELETE", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != 204 {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// Verify deleted
	req, _ = http.NewRequest("GET", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != 404 {
		t.Errorf("Expected status 404 after delete, got %d", resp.StatusCode)
	}
}

func TestMessageFlow(t *testing.T) {
	// First create a tenant
	reqBody := map[string]interface{}{
		"name":    "msg-test-tenant",
		"workers": 2,
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/tenants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	var tenant map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tenant)
	tenantID := tenant["id"].(string)
	defer func() {
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
		app.Test(req)
	}()

	// Create message
	msgBody := map[string]interface{}{
		"tenant_id": tenantID,
		"payload": map[string]interface{}{
			"event": "test",
			"data":  map[string]interface{}{"key": "value"},
		},
	}
	body, _ = json.Marshal(msgBody)
	req, _ = http.NewRequest("POST", "/api/v1/messages", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	var message map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&message)
	msgID := message["id"].(string)

	// List messages
	req, _ = http.NewRequest("GET", fmt.Sprintf("/api/v1/messages?tenant_id=%s", tenantID), nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Get message
	req, _ = http.NewRequest("GET", fmt.Sprintf("/api/v1/messages/%s?tenant_id=%s", msgID, tenantID), nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestCursorPagination(t *testing.T) {
	// Create tenant
	reqBody := map[string]interface{}{
		"name":    "cursor-test-tenant",
		"workers": 1,
	}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/tenants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := app.Test(req)
	var tenant map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tenant)
	tenantID := tenant["id"].(string)
	defer func() {
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
		app.Test(req)
	}()

	// Create multiple messages
	for i := 0; i < 5; i++ {
		msgBody := map[string]interface{}{
			"tenant_id": tenantID,
			"payload":   map[string]interface{}{"index": i},
		}
		body, _ := json.Marshal(msgBody)
		req, _ := http.NewRequest("POST", "/api/v1/messages", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		app.Test(req)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Test pagination
	req, _ = http.NewRequest("GET", fmt.Sprintf("/api/v1/messages?tenant_id=%s&limit=2", tenantID), nil)
	resp, _ = app.Test(req)

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data, ok := result["data"].([]interface{})
	if !ok || len(data) != 2 {
		t.Errorf("Expected 2 items, got %d", len(data))
	}

	if result["has_more"] != true {
		t.Error("Expected has_more to be true")
	}

	if result["next_cursor"] == "" {
		t.Error("Expected next_cursor to be present")
	}
}
