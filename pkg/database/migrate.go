// File: pkg/database/migrate.go
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	// Драйверы должны быть импортированы там, где используется sql.Open,
	// или здесь, если этот пакет будет предоставлять и функцию OpenDB.
	// Для миграций достаточно, чтобы драйвер был зарегистрирован в main.
	// _ "github.com/lib/pq" // Можно оставить здесь или в main пакетах, использующих БД
)

// ApplyMigrations выполняет миграции базы данных с использованием goose.
// Принимает логгер, тип драйвера БД, DSN и путь к директории с миграциями.
func ApplyMigrations(logger *slog.Logger, dbDriver, dbDSN, migrationsDir string) error {
	if dbDSN == "" {
		logger.Debug("POSTGRES_DSN is not set, skipping migrations.") // Изменил на Debug для менее важного сообщения
		return nil
	}
	if migrationsDir == "" {
		logger.Warn("MIGRATIONS_DIR is not set or empty, skipping migrations.")
		return nil
	}

	logger.Info("Attempting to connect to database for migrations...", "driver", dbDriver, "migrations_dir", migrationsDir)

	// sql.Open не устанавливает соединение сразу.
	db, err := sql.Open(dbDriver, dbDSN)
	if err != nil {
		logger.Error("Failed to prepare SQL connection for migrations", "error", err)
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer db.Close()

	// Проверка фактического соединения с базой данных
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pingCancel()
	if err = db.PingContext(pingCtx); err != nil {
		logger.Error("Failed to ping database for migrations", "error", err)
		return fmt.Errorf("db.PingContext: %w", err)
	}
	logger.Info("Successfully connected to database for migrations.")

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
