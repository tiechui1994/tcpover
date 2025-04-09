FROM golang:1.23 AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o ./tcpover ./cmd/tcpover/main.go
RUN CGO_ENABLED=0 go install github.com/anacrolix/torrent/cmd/torrent@latest && mv /go/bin/torrent /app


FROM ubuntu:latest
WORKDIR /app
COPY --from=builder /app/tcpover .
COPY --from=builder /app/torrent /root
COPY --from=builder /app/docker/init.sh .
RUN bash init.sh
ENTRYPOINT mkdir /run/sshd && \
    /usr/sbin/sshd -f /etc/ssh/sshd_config && /app/tcpover -a -m -name=google -e=wss://tcpover.pages.dev/tcpdump/api/ssh