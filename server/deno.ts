import {Hono} from "https://deno.land/x/hono@v3.4.1/mod.ts";
import {Mutex} from "https://deno.land/x/async@v2.1.0/mutex.ts";

const app = new Hono();
const manageSocket: any = {}
const groupSocket: any = {}
const mutex = new Mutex()

const ruleManage = "manage"
const ruleAgent = "Agent"
const ruleConnector = "Connector"

const modeDirect = "direct"
const modeForward = "forward"
const modeDirectMux = "directMux"
const modeForwardMux = "forwardMux"

const protoWless = "Wless"
const protoVless = "Vless"

class EmendWebsocket {
    public socket: WebSocket
    public attrs: string

    constructor(socket: WebSocket, attrs: string) {
        this.socket = socket
        this.attrs = attrs
    }

    close(code?: number, reason?: string) {
        this.socket.close(code, reason)
    }

    send(chunk: string | ArrayBufferLike | ArrayBuffer) {
        this.socket.send(chunk)
    }
}

class WebSocketStream {
    public socket: EmendWebsocket;
    public readable: ReadableStream<Uint8Array>;
    public writable: WritableStream<Uint8Array>;

    constructor(socket: EmendWebsocket) {
        this.socket = socket;
        this.readable = new ReadableStream({
            start(controller) {
                socket.socket.onmessage = (event) => {
                    controller.enqueue(new Uint8Array(event.data));
                };
                socket.socket.onerror = (e: any) => {
                    console.log("<readable onerror>", e.message)
                    controller.error(e)
                };
                socket.socket.onclose = () => {
                    console.log("<readable onclose>:", socket.attrs)
                    controller.close()
                }
            },
            pull(controller) {
            },
            cancel() {
                socket.close(1000, socket.attrs + "readable cancel");
            },
        });
        this.writable = new WritableStream({
            start(controller) {
                socket.socket.onerror = (e: any) => {
                    console.log("<writable onerror>:", e.message)
                }
                socket.socket.onclose = () => {
                    console.log("<writable onclose>:" + socket.attrs)
                }
            },
            write(chunk, controller) {
                socket.send(chunk);
            },
            close() {
                socket.close(1000, socket.attrs + "writable close");
            },
            abort(e) {
                socket.close(1006, socket.attrs + "writable abort");
            },
        });
    }
}

function proxy(request: any, endpoint: string) {
    const headers = new Headers({})
    headers.set('host', (new URL(endpoint)).host)
    for (const [key, value] of request.headers.entries()) {
        if (key.toLowerCase() == 'host') {
            continue
        }
        headers.set(key, value)
    }
    console.log("req url:", endpoint)
    const init: RequestInit = {
        method: request.method,
        headers: headers
    }
    if (['POST', 'PUT'].includes(request.method.toUpperCase())) {
        init.body = request.body
    }

    return fetch(endpoint, init).then((response) => {
        return new Response(response.body, response)
    })
}

function parseProtoAddress(proto: string, buffer: any) {
    let port, hostname, remain
    switch (proto) {
        case protoVless:
            port = buffer[20] | buffer[19] << 8
            switch (buffer[21]) {
                case 1:
                    hostname = buffer.slice(22, 22 + 4).join(".")
                    remain = buffer.slice(22+4)
                    break
                case 3:
                    hostname = buffer.slice(22, 22 + 16).join(":")
                    remain = buffer.slice(22+16)
                    break
                case 2:
                    const length = buffer[22]
                    hostname = new TextDecoder().decode(buffer.slice(23, 23 + length))
                    remain = buffer.slice(23+length)
                    break
            }
            break
        default:
            const len = buffer[0]
            const address = new TextDecoder().decode(buffer.slice(1, 1+len))
            const tokens = address.split(":")
            if (tokens.length >= 2) {
                hostname = tokens[0].trim()
                port = parseInt(tokens[1].trim())
            } else {
                hostname = tokens[0].trim()
                port = 443
            }
            remain = buffer.slice(1+len)
            break
    }

    return {hostname, port, remain}
}

app.get("/api/ssh", async (c) => {
    const upgrade = c.req.headers.get("upgrade") || "";
    if (upgrade.toLowerCase() != "websocket") {
        return new Response("request isn't trying to upgrade to websocket.");
    }

    const url = new URL(c.req.raw.url)
    if (c.req.headers.has("proto")) {
        url.searchParams.append("proto", c.req.raw.headers.get("proto"))
    }
    if (c.req.headers.has('x-real-host')) {
        url.host = c.req.headers.get("x-real-host")
    } else {
        url.host = "echo.websocket.org"
        url.pathname = "/"
    }
    url.protocol = "wss"
    console.log("real wss", url.toString())

    const {response, socket} = Deno.upgradeWebSocket(c.req.raw)
    socket.onopen = () => {
        const local = new WebSocketStream(new EmendWebsocket(socket, `local`))
        const remoteSocket = new WebSocket(url.toString())
        remoteSocket.binaryType = "arraybuffer"
        const remote = new WebSocketStream(new EmendWebsocket(remoteSocket, `remote`))

        try {
            // important: after remote websocket on ready, can send data
            remoteSocket.onopen = () => {
                local.readable.pipeTo(remote.writable).catch((e) => {
                    console.error("local readable exception:", e.message)
                })
            }

            // in here, local websocket on ready, can send data
            remote.readable.pipeTo(local.writable).catch((e) => {
                console.error("remote readable exception:", e.message)
            })
        } catch (e) {
            local.socket.close()
            remote.socket.close()
            console.error("socket catch:", e.message)
        }
    }

    return response
})

app.get("/~/ssh", async (c) => {
    const upgrade = c.req.headers.get("upgrade") || "";
    if (upgrade.toLowerCase() != "websocket") {
        return new Response("request isn't trying to upgrade to websocket.");
    }

    const {headers} = c.req
    const name = c.req.query("name") || ""
    const rule = c.req.query("rule") || ""
    const mode = c.req.query("mode") || ""

    console.log(`name: ${name}, rule: ${rule}, mode: ${mode}`)

    // direct connect
    if ([ruleConnector, ruleAgent].includes(rule) && [modeDirect].includes(mode)) {
        const {response, socket} = Deno.upgradeWebSocket(c.req.raw)
        socket.onopen = () => {
            const proto = headers.get("proto")

            socket.onmessage = (event) => {
                const {hostname, port, remain} = parseProtoAddress(proto, new Uint8Array(event.data))
                if (proto === protoVless) {
                    socket.send(new Uint8Array(2))
                }
                if (port === 443) {
                    socket.close(1006, "not support")
                    return
                }

                console.info(hostname, port)
                const local = new WebSocketStream(new EmendWebsocket(socket, `${rule}`))
                let conn = Deno.connect({
                    port: port,
                    hostname: hostname,
                })
                conn.then((remote) => {
                    if (remain && remain.byteLength > 0) {
                        const writer = conn.writable.getWriter()
                        writer.write(remain).then(() => {
                            writer.releaseLock()
                            local.readable.pipeTo(remote.writable).catch((e) => {
                                console.log("local readable exception:", e.message)
                            })
                            remote.readable.pipeTo(local.writable).catch((e) => {
                                console.log("remote readable exception:", e.message)
                            })
                        })
                        return
                    }

                    local.readable.pipeTo(remote.writable).catch((e) => {
                        console.log("local readable exception:", e.message)
                    })
                    remote.readable.pipeTo(local.writable).catch((e) => {
                        console.log("remote readable exception:", e.message)
                    })
                }).catch((e) => {
                    local.socket.close()
                    console.log("socket catch:", e.message)
                })
            }
        }

        return response
    }

    // forward connect
    if ([ruleConnector, ruleAgent].includes(rule) && [modeForward, modeForwardMux].includes(mode)) {
        let code = c.req.query("code") || ""

        if (name !== "") {
            const targetConn = manageSocket[name]
            if (!targetConn) {
                console.log("the target Agent websocket not exist", uid)
                return new Response("agent not running.");
            }

            code = (new Date()).toISOString()
            const data = JSON.stringify({
                Command: 0x01,
                Data: {
                    Code: code,
                    Mux: mode === modeForwardMux,
                    Network: "tcp",
                    Proto: headers.get("proto") || protoWless
                }
            })
            console.log("send data:", data)
            targetConn.send(data)
        }

        const {response, socket} = Deno.upgradeWebSocket(c.req.raw)
        await mutex.acquire();
        if (groupSocket[code]) {
            socket.onopen = () => {
                groupSocket[code].second = new WebSocketStream(new EmendWebsocket(socket, `${rule}_${code}`))
                const connPair = groupSocket[code]
                mutex.release()

                const first = connPair.first
                const second = connPair.second
                first.readable.pipeTo(second.writable).catch((e) => {
                    console.log("socket exception", first.socket.attrs, e.message)
                    groupSocket[code] = null
                })
                second.readable.pipeTo(first.writable).catch((e) => {
                    console.log("socket exception", second.socket.attrs, e.message)
                    groupSocket[code] = null
                })
            }
        } else {
            socket.onopen = () => {
                groupSocket[code] = {
                    first: new WebSocketStream(new EmendWebsocket(socket, `${rule}_${code}`)),
                }
                mutex.release()
            }
        }

        return response
    }

    // manage connect
    if ([ruleManage].includes(rule)) {
        const {response, socket} = Deno.upgradeWebSocket(c.req.raw)
        socket.onopen = () => {
            manageSocket[name] = new EmendWebsocket(socket, `${rule}_${name}`)
        }
        socket.onerror = (e) => {
            console.log("socket onerror", e.message, socket.extensions);
            manageSocket[name] = null
        }
        socket.onclose = () => {
            console.log("socket closed", socket.extensions);
            manageSocket[name] = null
        }
        return response
    }

    return new Response("request not support.");
})

app.on(['GET', 'DELETE', 'HEAD', 'OPTIONS', 'PUT', 'POST'], "*", async (c) => {
    const request = c.req.raw
    const url = new URL(request.url)
    const path = url.pathname + url.search

    let endpoint = ""
    if (path.startsWith("/https://") || path.startsWith("/http://")) {
        endpoint = path.substring(1)
    } else if (path.startsWith("/api")) {
        endpoint = "https://api.quinn.eu.org" + path
    } else {
        endpoint = "https://cloud.quinn.eu.org"
    }

    return await proxy(request, endpoint)
})

Deno.serve(app.fetch);

