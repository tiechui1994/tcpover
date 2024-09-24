// +heroku goVersion go1.17
// +heroku install ./cmd/tcpover

module github.com/tiechui1994/tcpover

go 1.17

require (
	github.com/gorilla/websocket v1.5.3
	github.com/tiechui1994/tool v1.5.12
	golang.org/x/net v0.29.0
)

require (
	github.com/sirupsen/logrus v1.9.0 // indirect
	golang.org/x/sys v0.25.0 // indirect
	golang.org/x/text v0.18.0 // indirect
)
