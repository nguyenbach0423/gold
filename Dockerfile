FROM golang:1.25.1-alpine AS builder

WORKDIR /bot

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gold .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata && \
    update-ca-certificates && \
    addgroup -g 1000 -S bot && \
    adduser -u 1000 -S -D -G bot -s /sbin/nologin bot

WORKDIR /bot

COPY --chown=bot:bot --from=builder /bot/gold /bot/gold

USER bot

EXPOSE 2000

ENTRYPOINT ["./gold"]