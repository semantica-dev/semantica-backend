# File: build/orchestrator/Dockerfile

# --- Этап сборки (builder) ---
FROM golang:1.23-alpine AS builder

# Устанавливаем рабочую директорию внутри контейнера
WORKDIR /app

# Устанавливаем необходимые инструменты для сборки, если они понадобятся
# Alpine не имеет ca-certificates по умолчанию, они могут быть нужны для скачивания модулей
RUN apk add --no-cache git ca-certificates tzdata

# Копируем файлы go.mod и go.sum для кэширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь остальной исходный код проекта
# Это включает pkg/, internal/, cmd/, db/migrations/ и т.д.
COPY . .

# Собираем бинарник Оркестратора
# CGO_ENABLED=0 для статической линковки и отсутствия зависимостей от C библиотек
# -ldflags="-w -s" для уменьшения размера бинарника (удаляет отладочную информацию)
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /app/orchestrator-service ./cmd/orchestrator

# --- Этап выполнения (runner) ---
FROM alpine:latest

# Устанавливаем рабочую директорию
WORKDIR /app

# Устанавливаем ca-certificates, если сервис будет делать HTTPS запросы к внешним ресурсам
# и tzdata для корректной работы с временными зонами
RUN apk add --no-cache ca-certificates tzdata

# Копируем скомпилированный бинарник Оркестратора с этапа сборки
COPY --from=builder /app/orchestrator-service /app/orchestrator-service

# Копируем директорию с миграциями.
# Оркестратор будет применять миграции, поэтому они ему нужны.
COPY --from=builder /app/db /app/db 
# Путь к миграциям внутри контейнера будет /app/db/migrations

# Указываем порт, который будет слушать приложение (для информации, не открывает порт автоматически)
EXPOSE 8080 
# Фактическое открытие порта будет в docker-compose.yaml

# Команда для запуска сервиса
ENTRYPOINT ["/app/orchestrator-service"]