FROM alpine:latest
WORKDIR /app
COPY . .
EXPOSE 8080
ENTRYPOINT [ "./tcpover", "-s", "-l", ":8080" ]
