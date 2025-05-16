# File: build/worker-crawler/Dockerfile

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

# Собираем бинарник Воркера-Краулера
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /app/worker-crawler-service ./cmd/worker-crawler

# --- Этап выполнения (runner) ---
FROM alpine:latest

# Устанавливаем рабочую директорию
WORKDIR /app

# Устанавливаем ca-certificates и tzdata
RUN apk add --no-cache ca-certificates tzdata

# Копируем скомпилированный бинарник Воркера-Краулера с этапа сборки
COPY --from=builder /app/worker-crawler-service /app/worker-crawler-service

# Воркерам обычно не нужно слушать порты, они работают с очередями.
# Поэтому EXPOSE здесь не нужен.

# Воркерам также не нужны файлы миграций (если они не управляют своей схемой БД).

# Команда для запуска сервиса
ENTRYPOINT ["/app/worker-crawler-service"]