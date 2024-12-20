FROM golang:1.17 AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o ./tcpover ./cmd/tcpover/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/tcpover .
EXPOSE 8080
ENTRYPOINT [ "./tcpover", "-s", "-l", ":8080" ]
