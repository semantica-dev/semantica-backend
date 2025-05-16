// File: pkg/database/migrate.go
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	// _ "github.com/lib/pq" // Драйвер импортируется в main
)

// ApplyMigrations выполняет миграции базы данных с использованием goose.
// Включает механизм повторных попыток для Ping.
func ApplyMigrations(logger *slog.Logger, dbDriver, dbDSN, migrationsDir string) error {
	if dbDSN == "" {
		logger.Debug("POSTGRES_DSN is not set, skipping migrations.")
		return nil
	}
	if migrationsDir == "" {
		logger.Warn("MIGRATIONS_DIR is not set or empty, skipping migrations.")
		return nil
	}

	logger.Info("Attempting to connect to database for migrations...", "driver", dbDriver, "migrations_dir", migrationsDir)

	db, err := sql.Open(dbDriver, dbDSN)
	if err != nil {
		logger.Error("Failed to prepare SQL connection for migrations", "error", err)
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer db.Close()

	// Механизм повторных попыток для Ping
	const maxPingRetries = 5
	const pingRetryInterval = 3 * time.Second
	var pingErr error

	for attempt := 1; attempt <= maxPingRetries; attempt++ {
		logger.Info("Pinging database...", "attempt", attempt, "max_attempts", maxPingRetries)
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second) // Таймаут для одной попытки Ping
		pingErr = db.PingContext(pingCtx)
		pingCancel() // Освобождаем ресурсы контекста

		if pingErr == nil {
			logger.Info("Successfully connected to database for migrations (ping successful).")
			break // Успех
		}

		logger.Warn("Failed to ping database", "attempt", attempt, "error", pingErr)
		if attempt < maxPingRetries {
			logger.Info("Retrying ping after interval", "interval", pingRetryInterval.String())
			time.Sleep(pingRetryInterval)
		}
	}

	if pingErr != nil { // Если после всех попыток Ping не удался
		logger.Error("Failed to ping database after multiple retries", "max_attempts", maxPingRetries, "error", pingErr)
		return fmt.Errorf("failed to ping database after %d attempts: %w", maxPingRetries, pingErr)
	}

	logger.Info("Setting goose dialect...", "dialect", dbDriver)
	if err := goose.SetDialect(dbDriver); err != nil {
		logger.Error("Failed to set goose dialect", "error", err)
		return fmt.Errorf("goose.SetDialect: %w", err)
	}

	logger.Info("Running goose migrations...", "direction", "up")
	if err := goose.Up(db, migrationsDir); err != nil {
		logger.Error("Goose Up migrations failed", "error", err)
		return fmt.Errorf("goose.Up: %w", err)
	}

	logger.Info("Goose migrations applied successfully.")
	return nil
}
