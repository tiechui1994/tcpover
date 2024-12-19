FROM ubuntu:latest
WORKDIR /app
COPY . .
RUN bash ./docker/init.sh
ENTRYPOINT [ "./tcpover", "-a", "-m", "-l=:8080", "-name=google", "-e=wss://tcpover.pages.dev/tcpdump/api/ssh" ]
