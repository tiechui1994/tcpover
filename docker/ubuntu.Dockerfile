FROM golang:1.22-alpine AS builder
RUN apk add build-base musl-dev
WORKDIR /app
COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/tcpover ./cmd/tcpover && \
    file /app/tcpover


FROM ubuntu:latest
WORKDIR /app
COPY --from=builder /app/tcpover .
COPY --from=builder /app/docker/init.sh .
RUN bash init.sh
ENTRYPOINT mkdir /run/sshd && \
    /usr/sbin/sshd -f /etc/ssh/sshd_config && /app/tcpover -a -m -name=google -e=wss://tcpover.pages.dev/tcpdump/api/ssh