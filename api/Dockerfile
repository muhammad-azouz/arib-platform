FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /license-api ./cmd/api

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /license-api .
EXPOSE 8080
CMD ["./license-api"]
