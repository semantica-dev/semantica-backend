// File: pkg/config/config.go
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config хранит всю конфигурацию приложения, загружаемую из переменных окружения.
type Config struct {
	// RabbitMQ
	RabbitMQ_URL string // Ожидается полная строка подключения, например "amqp://user:pass@host:port/"

	// Orchestrator
	OrchestratorAPIPort string // Внутренний порт Оркестратора, который слушает HTTP сервер, например ":8080"

	// PostgreSQL
	PostgresDSN string // Ожидается полная строка DSN, например "postgresql://user:pass@host:port/dbname?sslmode=disable"

	// Minio
	MinioEndpoint        string // Ожидается хост:порт, например "minio:9000"
	MinioAccessKeyID     string
	MinioSecretAccessKey string
	MinioUseSSL          bool
	MinioBucketName      string

	// Migrations
	MigrationsDir string // Путь к директории с файлами миграций

	// Общие настройки для Go-сервисов
	LogLevel      string        // Уровень логирования, например "INFO", "DEBUG", "WARN", "ERROR"
	LogFormat     string        // Формат логирования: "json" или "pretty"
	MaxRetries    int           // Максимальное количество попыток для retry-механизмов
	RetryInterval time.Duration // Интервал между попытками
}

// getEnv читает переменную окружения или возвращает значение по умолчанию.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// getEnvAsInt читает переменную окружения как int или возвращает значение по умолчанию.
func getEnvAsInt(key string, fallback int) int {
	strValue := getEnv(key, "")
	if value, err := strconv.Atoi(strValue); err == nil {
		return value
	}
	return fallback
}

// getEnvAsBool читает переменную окружения как bool или возвращает значение по умолчанию.
func getEnvAsBool(key string, fallback bool) bool {
	strValue := getEnv(key, "")
	if value, err := strconv.ParseBool(strValue); err == nil {
		return value
	}
	return fallback
}

// getEnvAsDuration читает переменную окружения как time.Duration или возвращает значение по умолчанию.
func getEnvAsDuration(key string, fallback time.Duration) time.Duration {
	strValue := getEnv(key, "")
	if value, err := time.ParseDuration(strValue); err == nil {
		return value
	}
	return fallback
}

// LoadConfig загружает конфигурацию из переменных окружения.
// Значения по умолчанию указаны для удобства локального запуска с docker-compose,
// где имена сервисов (например, "rabbitmq", "postgres", "minio") резолвятся во внутренние IP Docker-сети.
func LoadConfig() *Config {
	return &Config{
		// RabbitMQ
		RabbitMQ_URL: getEnv("RABBITMQ_URL", ""),

		// Orchestrator
		OrchestratorAPIPort: getEnv("ORCHESTRATOR_API_PORT", ":8080"),

		// PostgreSQL
		PostgresDSN: getEnv("POSTGRES_DSN", ""),

		// Minio
		MinioEndpoint:        getEnv("MINIO_ENDPOINT", "minio:9000"),
		MinioAccessKeyID:     getEnv("MINIO_ACCESS_KEY_ID", ""),
		MinioSecretAccessKey: getEnv("MINIO_SECRET_ACCESS_KEY", ""),
		MinioUseSSL:          getEnvAsBool("MINIO_USE_SSL", false),
		MinioBucketName:      getEnv("MINIO_BUCKET_NAME", "semantica-data"),

		// Migrations
		MigrationsDir: getEnv("MIGRATIONS_DIR", "/app/db/migrations"),

		// Общие
		LogLevel:      getEnv("LOG_LEVEL", "INFO"),
		LogFormat:     getEnv("LOG_FORMAT", "json"), // Default to "json"
		MaxRetries:    getEnvAsInt("APP_MAX_RETRIES", 5),
		RetryInterval: getEnvAsDuration("APP_RETRY_INTERVAL", 5*time.Second),
	}
}

// GetSlogLevel converts string log level (e.g., "INFO", "DEBUG") to slog.Level.
func (c *Config) GetSlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // Default to info if unknown or empty
	}
}
