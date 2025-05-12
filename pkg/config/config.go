package config

import (
	"os"
)

type Config struct {
	RabbitMQ_URL string
	// Для Оркестратора
	Orchestrator_API_Port string
}

func LoadConfig() *Config {
	cfg := &Config{
		RabbitMQ_URL:          getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		Orchestrator_API_Port: getEnv("ORCHESTRATOR_API_PORT", ":8080"),
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
