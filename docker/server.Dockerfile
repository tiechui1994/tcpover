FROM golang:1.22 AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o ./tcpover ./cmd/tcpover/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/tcpover .

RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid 10014 \
  "choreo"
# Use the above created unprivileged user
USER 10014

EXPOSE 8080
ENTRYPOINT [ "./tcpover", "-s", "-l", ":8080" ]
