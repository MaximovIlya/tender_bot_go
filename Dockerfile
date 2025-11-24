# Build stage
FROM golang:1.24.4-alpine AS builder 

WORKDIR /app

# Копируем файлы модулей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -o bot .

# Runtime stage (используем публичный Alpine, чтобы избежать авторизации в gcr.io)
FROM alpine:3.20

WORKDIR /app

# Устанавливаем сертификаты для https-запросов
RUN apk add --no-cache ca-certificates && \
    adduser -S -D -H botuser

# Копируем бинарник
COPY --from=builder /app/bot /app/bot
COPY --from=builder /app/.env /app/.env
COPY --from=builder /app/db/migrations /app/db/migrations

USER botuser

# Запускаем приложение
ENTRYPOINT ["/app/bot"]