# File: build/worker-extractor-other/Dockerfile

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

# Собираем бинарник Воркера-Экстрактора Других Файлов
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /app/worker-extractor-other-service ./cmd/worker-extractor-other

# --- Этап выполнения (runner) ---
FROM alpine:latest

# Устанавливаем рабочую директорию
WORKDIR /app

# Устанавливаем ca-certificates и tzdata
RUN apk add --no-cache ca-certificates tzdata

# Копируем скомпилированный бинарник
COPY --from=builder /app/worker-extractor-other-service /app/worker-extractor-other-service

# Команда для запуска сервиса
ENTRYPOINT ["/app/worker-extractor-other-service"]