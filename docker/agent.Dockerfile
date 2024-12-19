FROM golang:1.17-alpine AS builder
RUN apk add build-base
WORKDIR /app
COPY . .
RUN go mod tidy -compat=1.17
RUN go build -o ./tcpover ./cmd/tcpover/main.go


FROM alpine:latest AS runner
WORKDIR /app
COPY --from=builder /app/tcpover .
EXPOSE 8080
ENTRYPOINT ["./tcpover", "-a", "-m", "-l=:8080", "-name=google", "-e=wss://tcpover.pages.dev/tcpdump/api/ssh"]
