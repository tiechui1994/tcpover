FROM golang:1.17 AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o ./tcpover ./cmd/tcpover/main.go


FROM ubuntu:latest
WORKDIR /app
COPY --from=builder /app/tcpover .
COPY --from=builder /app/docker/init.sh .
RUN bash init.sh
ENTRYPOINT mkdir /run/sshd && \
    /usr/sbin/sshd -f /etc/ssh/sshd_config && /app/tcpover -a -m -name=google -e=wss://tcpover.pages.dev/tcpdump/api/ssh