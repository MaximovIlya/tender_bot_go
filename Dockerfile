# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Копируем файлы модулей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -a -installsuffix cgo -o tender-bot .

# Runtime stage
FROM gcr.io/distroless/static-debian11

WORKDIR /

# Копируем бинарник
COPY --from=builder /app/tender-bot /tender-bot
COPY --from=builder /app/.env /.env

# Запускаем приложение
CMD ["/tender-bot"]