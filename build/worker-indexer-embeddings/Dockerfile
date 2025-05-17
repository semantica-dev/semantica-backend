# File: build/worker-indexer-embeddings/Dockerfile

# --- Этап сборки (builder) ---
FROM golang:1.23-alpine AS builder

# Устанавливаем рабочую директорию внутри контейнера
WORKDIR /app

# Устанавливаем необходимые инструменты для сборки
RUN apk add --no-cache git ca-certificates tzdata

# Копируем файлы go.mod и go.sum для кэширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь остальной исходный код проекта
COPY . .

# Собираем бинарник Воркера-Индексатора Эмбеддингов
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /app/worker-indexer-embeddings-service ./cmd/worker-indexer-embeddings

# --- Этап выполнения (runner) ---
FROM alpine:latest

# Устанавливаем рабочую директорию
WORKDIR /app

# Устанавливаем ca-certificates и tzdata
RUN apk add --no-cache ca-certificates tzdata

# Копируем скомпилированный бинарник
COPY --from=builder /app/worker-indexer-embeddings-service /app/worker-indexer-embeddings-service

# Команда для запуска сервиса
ENTRYPOINT ["/app/worker-indexer-embeddings-service"]