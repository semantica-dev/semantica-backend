// File: pkg/config/config.go
package config

import (
	"os"
	"strconv"
	"time"
)

// Config хранит всю конфигурацию приложения, загружаемую из переменных окружения.
type Config struct {
	// RabbitMQ
	RabbitMQ_URL string

	// Orchestrator
	OrchestratorAPIPort string // Внутренний порт Оркестратора, например ":8080"

	// PostgreSQL (для Оркестратора и других сервисов, если нужно)
	PostgresDSN string

	// Minio
	MinioEndpoint        string
	MinioAccessKeyID     string
	MinioSecretAccessKey string
	MinioUseSSL          bool
	MinioBucketName      string

	// Migrations (для сервисов, которые их запускают, например, Оркестратор)
	MigrationsDir string

	// Общие настройки для Go-сервисов
	LogLevel      string
	MaxRetries    int
	RetryInterval time.Duration
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	strValue := getEnv(key, "")
	if value, err := strconv.Atoi(strValue); err == nil {
		return value
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	strValue := getEnv(key, "")
	if value, err := strconv.ParseBool(strValue); err == nil {
		return value
	}
	return fallback
}

func getEnvAsDuration(key string, fallback time.Duration) time.Duration {
	strValue := getEnv(key, "")
	if value, err := time.ParseDuration(strValue); err == nil {
		return value
	}
	return fallback
}

// LoadConfig загружает конфигурацию из переменных окружения.
func LoadConfig() *Config {
	return &Config{
		// RabbitMQ
		RabbitMQ_URL: getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"), // Имя сервиса 'rabbitmq'

		// Orchestrator
		OrchestratorAPIPort: getEnv("ORCHESTRATOR_API_PORT", ":8080"), // Это порт, который слушает Go приложение

		// PostgreSQL
		PostgresDSN: getEnv("POSTGRES_DSN", ""), // DSN должен быть полным, например: "postgresql://user:pass@postgres:5432/dbname?sslmode=disable"

		// Minio
		MinioEndpoint:        getEnv("MINIO_ENDPOINT", "http://minio:9000"), // Имя сервиса 'minio'
		MinioAccessKeyID:     getEnv("MINIO_ACCESS_KEY_ID", "minioadmin"),
		MinioSecretAccessKey: getEnv("MINIO_SECRET_ACCESS_KEY", "minioadmin"), // В .env должны быть другие значения
		MinioUseSSL:          getEnvAsBool("MINIO_USE_SSL", false),
		MinioBucketName:      getEnv("MINIO_BUCKET_NAME", "semantica-data"),

		// Migrations
		MigrationsDir: getEnv("MIGRATIONS_DIR", "db/migrations"), // Путь внутри контейнера будет /app/db/migrations

		// Общие
		LogLevel:      getEnv("LOG_LEVEL", "INFO"),
		MaxRetries:    getEnvAsInt("APP_MAX_RETRIES", 5),
		RetryInterval: getEnvAsDuration("APP_RETRY_INTERVAL", 5*time.Second),
	}
}
