import {Hono} from "https://deno.land/x/hono@v3.4.1/mod.ts";
import {Mutex} from "https://deno.land/x/async@v2.1.0/mutex.ts";

const app = new Hono();
const manageSocket: any = {}
const groupSocket: any = {}
const mutex = new Mutex()

const ruleManage = "manager"
const ruleAgent = "Agent"
const ruleConnector = "Connector"

const modeDirect = "direct"
const modeForward = "forward"
const modeDirectMux = "directMux"
const modeForwardMux = "forwardMux"

const protoWless = "Wless"
const protoVless = "Vless"

const decoder = new TextDecoder()

const debug = false
const warn = (...data: any[]) => {
    if (debug) console.warn(data)
}
const error = (...data: any[]) => {
    if (debug) console.error(data)
}
const info =  (...data: any[]) => {
    if (debug) console.info(data)
}

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
                socket.socket.onmessage = (event) =>  controller.enqueue(new Uint8Array(event.data));
                socket.socket.onerror = (e: any) => {
                    error("<readable onerror>", e.message)
                    controller.error(e)
                };
            },
            pull(controller) {
            },
            cancel() {
                socket.close(1000, socket.attrs + "readable cancel");
            },
        });
        this.writable = new WritableStream({
            start(controller) {
                socket.socket.onerror = (e: any) => error("<writable onerror>:", e.message)
            },
            write(chunk, controller) {
                socket.send(chunk);
            },
            close() {
                socket.close();
            },
            abort(e) {
                socket.close(1006, socket.attrs + "writable abort");
            },
        });
    }
}

class Buffer {
    private end = 0;
    private readonly data: Uint8Array;

    constructor(size = 8192) {
        this.data = new Uint8Array(size);
    }

    write(data: Uint8Array, length: number) {
        this.data.set(data.subarray(0, length), this.end);
        this.end += length;
    }

    // 返回当前的视图，不产生内存拷贝
    bytes() {
        return this.data.subarray(0, this.end);
    }

    at(index: number) {
        return this.data[index];
    }

    subarray(start: number, end?: number) {
        return this.data.subarray(start, end ?? this.end);
    }

    length() {
        return this.end;
    }

    reset() {
        this.end = 0;
    }
}

const SYN = 0x00
const FIN = 0x01
const PSH = 0x02
const NOP = 0x03

class MuxSocketStream {
    private stream: WebSocketStream
    private readonly sessions: Record<number, any>;

    constructor(stream) {
        this.stream = stream;
        this.sessions = {}
        this.run().catch((err) => {
            warn("run::catch", err.stack)
            this.stream.socket.close(1000)
        })
    }


    // @ts-ignore
    async run() {
        const socket = this.stream.socket
        const sessions = this.sessions
        const merge = this.merge
        const parseMuxAddress = this.parseMuxAddress
        const handleRemoteToLocal = this.handleRemoteToLocal
        const closeSession = (id: number) => {
            const session = sessions[id];
            if (session) {
                const headerBuf = new Uint8Array(8);
                const view = new DataView(headerBuf.buffer);
                view.setUint8(0,1)
                view.setUint8(1, FIN)
                view.setUint16(2, 0,true)
                view.setUint32(4,id,true)
                socket.send(view.buffer)
                delete this.sessions[id];
            }
        }

        // mux:
        // 1) vless request
        // 2) mux version info: version(1) protocol(1, smux:00)
        // 3) mux header (SYN)
        // 4) mux header [SYNC Header]
        //
        // vless first packet
        // version(1) uuid(16) addon(1) command(1) port(2) addrType(1)
        //
        // mux header
        // ver(1) + cmd(1) + length(2) + id(4) + data(N)
        // cmd: SYN(0), FIN(1), PSH(2), NOP(3), UDP(4)
        //
        // SYNC Header
        // flags(2) + type(1) + addr(4+2, 16+2, 1+2+N)

        const vlessHeaderLen = 1 + 16 + 1 + 1 + 2 + 1
        const muxHeaderLen = 8
        const firstBuffer = new Buffer()
        let readRequest = true
        let remainChunk

        const writableStream = new WritableStream({
            start(controller) {
            },
            // @ts-ignore
            async write(chunk, controller) {
                if (readRequest) {
                    firstBuffer.write(chunk, chunk.byteLength)
                    // 1) vless request
                    if (firstBuffer.length() < vlessHeaderLen) return

                    let realLen = vlessHeaderLen
                    const addrType = firstBuffer.at(vlessHeaderLen - 1)
                    if (addrType == 1) realLen+=4;
                    else if (addrType==3) realLen+=16;
                    else if (addrType==2) realLen += 1 + firstBuffer.at(vlessHeaderLen);

                    // 2) mux version info, 2 bytes
                    if (firstBuffer.length() < realLen + 2) return

                    realLen  += 2

                    socket.send(new Uint8Array(2))
                    readRequest = false
                    chunk = firstBuffer.subarray(realLen)
                    if (chunk.byteLength === 0) return
                }

                if (remainChunk && remainChunk.byteLength > 0) {
                    chunk = merge(remainChunk, chunk.subarray(muxHeaderLen))
                    remainChunk = null
                }

                // 3) mux header
                const view = new DataView(chunk.buffer, chunk.byteOffset, chunk.byteLength);
                const sessionID = view.getUint32(4,true) // 4, 5, 6, 7
                const length = view.getUint16(2, true) // 2, 3
                const command = view.getUint8(1) // 1

                if (command === SYN) {
                    if (chunk.byteLength <= muxHeaderLen + vlessHeaderLen) {
                        remainChunk = chunk
                        return
                    }

                    chunk = chunk.subarray(muxHeaderLen)
                    const {hostname, port, offset} = parseMuxAddress(chunk, new DataView(chunk.buffer, chunk.byteOffset))
                    chunk = chunk.subarray(offset)
                    const conn = await Deno.connect({
                        port: port,
                        hostname: hostname,
                    })
                    info(`${hostname}:${port}`)
                    const writer = conn.writable.getWriter()
                    if (chunk.byteLength > 0) {
                        await writer.write(chunk)
                    }
                    sessions[sessionID] = {conn: conn, writer: writer}

                    handleRemoteToLocal(conn, sessionID, socket).catch((err) => {
                        warn("connect::catch", err.stack)
                        closeSession(sessionID)
                    }).then(() => {
                        closeSession(sessionID)
                    })
                }

                if (command === FIN) {
                    closeSession(sessionID)
                    return;
                }

                if (command === NOP) {
                    return;
                }

                if (command === PSH) {
                    if (sessions[sessionID]) {
                        const {writer} = sessions[sessionID]
                        if (writer) {
                            await writer.write(chunk.subarray(muxHeaderLen, muxHeaderLen + length))
                        }
                    }
                }
            },
            close() {
            },
            abort(e) {
            },
        });

        await this.stream.readable.pipeTo(writableStream).catch((e) => {
            warn("read write", e.stack)
        })
    }

    async handleRemoteToLocal(conn: Deno.TcpConn, id: number, socket: EmendWebsocket) {
        const headerBuf = new Uint8Array(8);
        const view = new DataView(headerBuf.buffer);
        let first = true;

        for await (const raw of conn.readable) {
            let chunk = raw;
            // 响应首包特殊处理 (根据原逻辑补 0)
            if (first) {
                const tmp = new Uint8Array(raw.byteLength + 1);
                tmp.set(raw, 1);
                chunk = tmp;
                first = false;
            }

            // 分段发送，避免单个包过大
            let index = 0;
            while (index < chunk.byteLength) {
                const size = Math.min(chunk.byteLength - index, 2048);
                view.setUint8(0, 1);
                view.setUint8(1, PSH); // PSH
                view.setUint16(2, size, true);
                view.setUint32(4, id, true);

                socket.send(headerBuf)
                socket.send(chunk.subarray(index, index + size));
                //const fullPacket = new Uint8Array(8 + size);
                //fullPacket.set(headerBuf, 0);
                //fullPacket.set(chunk.subarray(index, index + size), 8);
                //socket.send(fullPacket);
                index += size;
            }
        }
    }

    parseMuxAddress(chunk: Uint8Array, view: DataView) {
        let hostname = "", port = 0, offset = 0;
        const type = chunk[2];
        if (type === 1) { // IPv4
            hostname = chunk.subarray(3, 3+4).join(".");
            port = view.getUint16(3+4, false); // 大端
            offset = 9;
        } else if (type === 3) { // Domain
            const len = chunk[3];
            hostname = decoder.decode(chunk.subarray(4, 4 + len));
            port = view.getUint16(4 + len, false);
            offset = 4+2 + len;
        } else if (type == 2) {
            hostname = chunk.subarray(3, 3+16).join(".");
            port = view.getUint16(3+16, false); // 大端
            offset = 21;
        }
        return { hostname, port, offset };
    }

    merge(a: Uint8Array, b: Uint8Array) {
        const res = new Uint8Array(a.byteLength + b.byteLength);
        res.set(a, 0);
        res.set(b, a.byteLength);
        return res;
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
    let port, hostname = ""
    switch (proto) {
        case protoVless:
            port = buffer[20] | buffer[19] << 8
            switch (buffer[21]) {
                case 1:
                    hostname = buffer.subarray(22, 23 + 4).join(".")
                    break
                case 3:
                    hostname = buffer.subarray(22, 23 + 16).join(":")
                    break
                case 2:
                    const length = buffer[22]
                    hostname = decoder.decode(buffer.subarray(23, 23 + length))
                    break
            }
            break
        default:
            const len = buffer[0]
            const address = decoder.decode(buffer.subarray(1, 1+len))
            const tokens = address.split(":")
            if (tokens.length >= 2) {
                hostname = tokens[0].trim()
                port = parseInt(tokens[1].trim())
            } else {
                hostname = tokens[0].trim()
                port = 443
            }
            break
    }

    return {hostname, port}
}

async function socket(c) {
    const upgrade = c.req.headers.get("upgrade") || "";
    if (upgrade.toLowerCase() != "websocket") {
        return new Response("request isn't trying to upgrade to websocket.");
    }

    let binaryType
    const url = new URL(c.req.raw.url)
    if (c.req.headers.has("proto")) {
        url.searchParams.append("proto", c.req.raw.headers.get("proto"))
    }
    if (c.req.headers.has('x-real-host')) {
        url.host = c.req.headers.get("x-real-host")
        binaryType = "arraybuffer"
    } else {
        url.host = "echo.websocket.org"
        url.pathname = "/"
        binaryType = "blob"
    }
    url.protocol = "wss"
    console.log("real wss", url.toString())

    const {response, socket} = Deno.upgradeWebSocket(c.req.raw)
    socket.onopen = () => {
        const local = new WebSocketStream(new EmendWebsocket(socket, `local`))
        const remoteSocket = new WebSocket(url.toString())
        remoteSocket.binaryType = binaryType
        const remote = new WebSocketStream(new EmendWebsocket(remoteSocket, `remote`))

        try {
            // important: after remote websocket on ready, can send data
            remoteSocket.onopen = () => {
                local.readable.pipeTo(remote.writable).catch((e) => {
                    warn("local readable exception:", e.message)
                })
            }
            // in here, local websocket on ready, can send data
            remote.readable.pipeTo(local.writable).catch((e) => {
                warn("remote readable exception:", e.message)
            })
        } catch (e) {
            local.socket.close()
            remote.socket.close()
            warn("socket catch:", e.message)
        }
    }

    return response
}


app.get("/api/ssh", async (c) => {
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
    if ([ruleConnector, ruleAgent].includes(rule) && [modeDirect, modeDirectMux].includes(mode)) {
        const {response, socket} = Deno.upgradeWebSocket(c.req.raw)
        if (modeDirect === mode) {
            socket.onopen = () => {
                const proto = headers.get("proto")
                socket.onmessage = (event) => {
                    const {hostname, port} = parseProtoAddress(proto, new Uint8Array(event.data))
                    if (proto === protoVless) {
                        socket.send(new Uint8Array(2))
                    }

                    const conn = Deno.connect({
                        port: port,
                        hostname: hostname,
                    })
                    info(hostname, port)
                    const local = new WebSocketStream(new EmendWebsocket(socket, `direct`))
                    conn.then(remote => {
                        local.readable.pipeTo(remote.writable).catch((e) => {
                            warn("local exception:", e.message)
                        })
                        remote.readable.pipeTo(local.writable).catch((e) => {
                            warn("remote exception:", e.message);
                        })
                    }).catch((e) => {
                        local.socket.close()
                        conn.close()
                    })
                }
            }
        } else {
            new MuxSocketStream(new WebSocketStream(new EmendWebsocket(socket, "mux")))
        }

        return response
    }

    // forward connect
    if ([ruleConnector, ruleAgent].includes(rule) && [modeForward, modeForwardMux].includes(mode)) {
        let code = c.req.query("code") || ""
        if (name !== "") {
            const targetConn = manageSocket[name]
            if (!targetConn) {
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
                    warn("socket exception", first.socket.attrs, e.message)
                    groupSocket[code] = null
                })
                second.readable.pipeTo(first.writable).catch((e) => {
                    warn("socket exception", second.socket.attrs, e.message)
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
            warn("socket onerror", e.message, socket.extensions);
            manageSocket[name] = null
        }
        socket.onclose = () => {
            manageSocket[name] = null
        }
        return response
    }

    return new Response("request not support.");
})

app.on(['GET', 'DELETE', 'HEAD', 'OPTIONS', 'PUT', 'POST'], "*", async (c) => {
    const upgrade = c.req.headers.get("upgrade") || "";
    if (upgrade.toLowerCase() === "websocket") {
        return await socket(c)
    }

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
