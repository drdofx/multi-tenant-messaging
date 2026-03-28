package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	App        AppConfig
	Database   DatabaseConfig
	RabbitMQ   RabbitMQConfig
	JWT        JWTConfig
	Workers    WorkersConfig
	Monitoring MonitoringConfig
}

// AppConfig holds general application configuration
type AppConfig struct {
	Env  string
	Port string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	URL string
}

// RabbitMQConfig holds RabbitMQ configuration
type RabbitMQConfig struct {
	URL string
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret string
	Expiry time.Duration
}

// WorkersConfig holds worker pool configuration
type WorkersConfig struct {
	Default    int
	Max        int
	MaxRetry   int
	MessageTTL time.Duration
}

// MonitoringConfig holds monitoring configuration
type MonitoringConfig struct {
	Port string
	Path string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	cfg := &Config{}

	// App config
	cfg.App.Env = getEnv("APP_ENV", "development")
	cfg.App.Port = getEnv("APP_PORT", "8080")

	// Database config
	cfg.Database.URL = getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/multitenant?sslmode=disable")

	// RabbitMQ config
	cfg.RabbitMQ.URL = getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	// JWT config
	cfg.JWT.Secret = getEnv("JWT_SECRET", "")
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	expiryStr := getEnv("JWT_EXPIRY", "24h")
	expiry, err := time.ParseDuration(expiryStr)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_EXPIRY: %w", err)
	}
	cfg.JWT.Expiry = expiry

	// Workers config
	cfg.Workers.Default = getEnvAsInt("DEFAULT_WORKERS", 3)
	cfg.Workers.Max = getEnvAsInt("MAX_WORKERS", 50)
	cfg.Workers.MaxRetry = getEnvAsInt("MAX_RETRY_COUNT", 3)

	ttlMs := getEnvAsInt("MESSAGE_TTL_MS", 30000)
	cfg.Workers.MessageTTL = time.Duration(ttlMs) * time.Millisecond

	// Monitoring config
	cfg.Monitoring.Port = getEnv("METRICS_PORT", "9090")
	cfg.Monitoring.Path = getEnv("METRICS_PATH", "/metrics")

	return cfg, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as an integer or returns a default value
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
