# Multi-stage build для оптимизации размера образа

# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Устанавливаем необходимые build tools
RUN apk add --no-cache git ca-certificates

# Копируем go.mod и go.sum
COPY go.mod go.sum ./

# Загружаем зависимости
RUN go mod download

# Копируем весь код
COPY . .

# Компилируем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o flower-bot .

# Stage 2: Runtime
FROM alpine:3.18

WORKDIR /app

# Устанавливаем необходимые runtime зависимости
RUN apk --no-cache add ca-certificates tzdata

# Копируем скомпилированный бинарник из builder stage
COPY --from=builder /app/flower-bot .

# Создаем non-root пользователя для безопасности
RUN addgroup -g 1000 appuser && adduser -D -u 1000 -G appuser appuser
USER appuser

# Запускаем приложение
CMD ["./flower-bot"]
