FROM golang:1.22-alpine AS builder
RUN apk add build-base musl-dev
WORKDIR /app
COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/tcpover ./cmd/tcpover && \
    file /app/tcpover


FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/tcpover .
ENV PORT=8080
EXPOSE 8080
CMD [ "/app/tcpover", "-s", "-l", ":8080" ]
