// @ts-ignore
import {connect} from 'cloudflare:sockets';

class EmendWebsocket {
    constructor(socket, attrs) {
        this.socket = socket
        this.attrs = attrs
    }

    close(code, reason) {
        this.socket.close(code, reason)
    }

    send(chunk) {
        this.socket.send(chunk)
    }
}

class Buffer {
    constructor() {
        this.start = 0
        this.end = 0
        this.data = new Uint8Array(8192)
    }

    write(data, length) {
        for (let i = 0; i < length; i++) {
            this.data[this.end] = data[i]
            this.end++
        }
    }

    bytes() {
        if (arguments.length > 0) {
            return this.data.slice(this.start, this.end)
        }
        return this.data.slice(this.start, this.end)
    }

    at(index) {
        return this.data[index]
    }

    slice(start, end) {
        if (end && end > 0) {
            return this.data.slice(start, end)
        }

        return this.data.slice(start, this.end)
    }

    length() {
        return this.end - this.start
    }

    reset() {
        this.start = 0
        this.end = 0
    }
}

class WebSocketStream {
    constructor(socket) {
        this.socket = socket
        this.readable = new ReadableStream({
            start(controller) {
                socket.socket.onmessage = (event) => {
                    controller.enqueue(new Uint8Array(event.data));
                };
                socket.socket.onerror = (e) => {
                    console.warn("<readable onerror>", e.message)
                    controller.error(e)
                }
                socket.socket.onclose = () => {
                    console.warn("<readable onclose>:", socket.attrs)
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
                socket.socket.onerror = (e) => {
                    console.warn("<writable onerror>:", e.message)
                }
                socket.socket.onclose = () => {
                    console.warn("<writable onclose>:" + socket.attrs)
                };
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

class MuxSocketStream {
    constructor(stream) {
        this.stream = stream;
        this.sessions = {}
        this.run().catch((err) => {
            console.warn("run::catch", err.stack)
            this.stream.socket.close(1000)
        })
    }

    async run() {
        const SYN = 0x00
        const FIN = 0x01
        const PSH = 0x02
        const NOP = 0x03

        const Mask = 255

        const socket = this.stream.socket
        const sessions = this.sessions
        const merge = this.merge

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
            async write(chunk, controller) {
                if (readRequest) {
                    firstBuffer.write(chunk, chunk.byteLength)
                    // 1) vless request
                    if (firstBuffer.length() < vlessHeaderLen) {
                        return
                    }
                    let firstPacketRealLength = vlessHeaderLen
                    const addrType = firstBuffer.at(vlessHeaderLen - 1)
                    switch (addrType) {
                        case 1:
                            if (firstBuffer.length() < vlessHeaderLen + 4) {
                                return
                            }
                            firstPacketRealLength = vlessHeaderLen + 4
                            break
                        case 3:
                            if (firstBuffer.length() < vlessHeaderLen + 16) {
                                return
                            }
                            firstPacketRealLength = vlessHeaderLen + 16
                            break
                        case 2:
                            const domainLength = firstBuffer.at(vlessHeaderLen)
                            if (firstBuffer.length() === vlessHeaderLen + domainLength) {
                                return
                            }
                            firstPacketRealLength = vlessHeaderLen + 1 + domainLength
                            break
                    }
                    socket.send(new Uint8Array(2))
                    console.info("send first request")

                    // 2) mux version info, 2 bytes
                    if (firstBuffer.length() < firstPacketRealLength + 2) {
                        return
                    }

                    firstPacketRealLength = firstPacketRealLength + 2
                    readRequest = false
                    chunk = firstBuffer.slice(firstPacketRealLength)
                    if (chunk.byteLength === 0) {
                        return
                    }
                }

                if (remainChunk && remainChunk.byteLength > 0) {
                    chunk = merge(remainChunk, chunk.slice(muxHeaderLen))
                    remainChunk = null
                }

                // 3) mux header
                const sessionID = chunk[7] << 24 | chunk[6] << 16 | chunk[5] << 8 | chunk[4]
                const length = chunk[3] << 8 | chunk[2]
                const command = chunk[1]

                if (command === SYN) {
                    if (chunk.byteLength <= muxHeaderLen + vlessHeaderLen) {
                        remainChunk = chunk
                        return
                    }

                    chunk = chunk.slice(muxHeaderLen)
                    let hostname, port
                    switch (chunk[2]) {
                        case 1:
                            hostname = chunk.slice(3, 3 + 4).join(".")
                            port = chunk[3 + 4 + 1] | chunk[3 + 4] << 8
                            chunk = chunk.slice(3 + 4 + 2)
                            break
                        case 4:
                            hostname = chunk.slice(3, 3 + 16).join(":")
                            port = chunk[3 + 16 + 1] | chunk[3 + 16] << 8
                            chunk = chunk.slice(3 + 16 + 2)
                            break
                        case 3:
                            const length = chunk[3]
                            hostname = new TextDecoder().decode(chunk.slice(4, 4 + length))
                            port = chunk[4 + length + 1] | chunk[4 + length] << 8
                            chunk = chunk.slice(4 + length + 2)
                            break
                    }
                    const current = {
                        id: sessionID,
                        buf: new Buffer(),
                        socket: socket
                    }
                    console.info(`domain: ${hostname}:${port}, ${current.id}`)
                    const conn = connect(`${hostname}:${port}`, {secureTransport: "off"})
                    const writer = conn.writable.getWriter()
                    if (chunk.byteLength > 0) {
                        await writer.write(chunk)
                    }


                    let sendResponse = true
                    sessions[current.id] = {
                        conn: conn,
                        writer: writer
                    }
                    conn.readable.pipeTo(new WritableStream({
                        start(controller) {
                        },
                        write(raw, controller) {
                            if (sendResponse) {
                                raw = merge([0], raw)
                                sendResponse = false
                            }
                            const N = raw.byteLength
                            let index = 0
                            while (index < N) {
                                current.buf.reset()

                                const size = index + 2048 < N ? 2048 : N - index
                                // length
                                const L1 = (size >> 8) & Mask
                                const L2 = size & Mask
                                // id
                                const I1 = (current.id >> 24) & Mask
                                const I2 = (current.id >> 16) & Mask
                                const I3 = (current.id >> 8) & Mask
                                const I4 = current.id & Mask

                                const header = new Uint8Array([1, PSH, L2, L1, I4, I3, I2, I1])
                                current.buf.write(header, header.length)
                                current.buf.write(raw.slice(index, index + size), size)
                                current.socket.send(current.buf.bytes())
                                index = index + size
                            }
                        },
                        close() {
                        },
                        abort(e) {
                        },
                    })).catch((err) => {
                        console.warn("connect::catch", err.stack)
                        current.buf.reset()
                        // id
                        const I1 = (current.id >> 24) & Mask
                        const I2 = (current.id >> 16) & Mask
                        const I3 = (current.id >> 8) & Mask
                        const I4 = current.id & Mask
                        const header = new Uint8Array([1, PSH, 0, 0, I4, I3, I2, I1])
                        current.socket.send(header)
                        delete sessions[current.id]
                    })

                    return
                }

                if (command === FIN) {
                    console.info(`StatusEnd end`)
                    delete sessions[sessionID]
                    return;
                }

                if (command === NOP) {
                    console.info(`StatusKeepAlive end`)
                    return;
                }

                if (command === PSH) {
                    chunk = chunk.slice(muxHeaderLen, muxHeaderLen + length)
                    const {conn, writer} = sessions[sessionID]
                    if (writer) {
                        await writer.write(chunk)
                    }
                }
            },
            close() {
            },
            abort(e) {
            },
        });

        await this.stream.readable.pipeTo(writableStream).catch((e) => {
            console.warn("read write", e)
        })
    }

    merge(a, b) {
        const aLen = a instanceof ArrayBuffer ? a.byteLength : a.length
        const bLen = b instanceof ArrayBuffer ? b.byteLength : b.length

        const merge = new Uint8Array(aLen + bLen)
        let index = 0
        for (let i = 0; i < aLen; i++) {
            merge[index++] = a[i]
        }
        for (let i = 0; i < bLen; i++) {
            merge[index++] = b[i]
        }

        a = null
        b = null
        return merge
    }
}

let uuid = null;

const ruleManage = "manage"
const ruleAgent = "Agent"
const ruleConnector = "Connector"

const modeDirect = "direct"
const modeForward = "forward"
const modeDirectMux = "directMux"
const modeForwardMux = "forwardMux"

const protoWless = "Wless"
const protoVless = "Vless"

function parseProtoAddress(proto, buffer) {
    console.info("data, ", proto, buffer.byteLength)
    let port, hostname, remain
    switch (proto) {
        case protoVless:
            port = buffer[20] | buffer[19] << 8
            switch (buffer[21]) {
                case 1:
                    hostname = buffer.slice(22, 22 + 4).join(".")
                    remain = buffer.slice(22 + 4)
                    break
                case 3:
                    hostname = buffer.slice(22, 22 + 16).join(":")
                    remain = buffer.slice(22 + 16)
                    break
                case 2:
                    const length = buffer[22]
                    hostname = new TextDecoder().decode(buffer.slice(23, 23 + length))
                    remain = buffer.slice(23 + length)
                    break
            }
            break
        default:
            const len = buffer[0]
            const address = new TextDecoder().decode(buffer.slice(1, 1 + len))
            const tokens = address.split(":")
            if (tokens.length >= 2) {
                hostname = tokens[0].trim()
                port = parseInt(tokens[1].trim())
            } else {
                hostname = tokens[0].trim()
                port = 443
            }
            remain = buffer.slice(1 + len)
            break
    }

    return {hostname, port, remain}
}

function check(request) {
    if (!uuid) {
        uuid = new Date();
    }
    return new Response("code is:" + uuid.valueOf())
}

function safeCloseWebSocket(socket) {
    try {
        if (socket.readyState === 1 || socket.readyState === 2) {
            socket.close();
        }
    } catch (error) {
        console.warn('safeCloseWebSocket error', error);
    }
}

async function websocket(request) {
    const url = new URL(request.url);

    const name = url.searchParams.get("name")
    const code = url.searchParams.get("code")
    const rule = url.searchParams.get("rule")
    const mode = url.searchParams.get("mode")

    if ([ruleConnector, ruleAgent].includes(rule) && [modeDirect, modeDirectMux].includes(mode)) {
        // @ts-ignore
        const webSocketPair = new WebSocketPair();
        const [client, webSocket] = Object.values(webSocketPair);
        webSocket.accept();

        const proto = request.headers.get("proto")
        if (modeDirect === mode) {
            webSocket.onmessage = async (event) => {
                console.info("proto", proto)
                const {hostname, port, remain} = parseProtoAddress(proto, new Uint8Array(event.data))
                if (proto === protoVless) {
                    webSocket.send(new Uint8Array(2))
                }

                const remote = new WebSocketStream(new EmendWebsocket(webSocket, `${rule}_${proto}}`))
                console.info(`real addr: ${hostname}:${port}, ${remain.byteLength}`)
                const local = connect(`${hostname}:${port}`, {secureTransport: "off"})
                if (remain && remain.byteLength > 0) {
                    const writer = local.writable.getWriter()
                    await writer.write(remain)
                    writer.releaseLock();
                }
                remote.readable.pipeTo(local.writable).catch((e) => {
                    console.warn("socket exception", e.message)
                    safeCloseWebSocket(webSocket)
                })
                local.readable.pipeTo(remote.writable).catch((e) => {
                    console.warn("socket exception", e.message)
                    safeCloseWebSocket(webSocket)
                })
            }
        } else {
            new MuxSocketStream(new WebSocketStream(new EmendWebsocket(webSocket, `${rule}_${proto}`)))
        }

        return new Response(null, {
            status: 101,
            webSocket: client,
        });
    }

    if (rule === ruleManage) {
        const webSocketPair = new WebSocketPair();
        const [client, webSocket] = Object.values(webSocketPair);
        webSocket.accept();

        webSocket.addEventListener("open", (event) => {
            new WebSocketStream(new EmendWebsocket(webSocket, `${rule}_${name}`))
        });

        return new Response(null, {
            status: 101,
            webSocket: client,
        });
    }

    return new Response("Bad Request", {
        status: 400,
    });
}


async function forward(request, u) {
    const url = new URL(u)
    request.headers['host'] = url.host
    let init = {
        method: request.method,
        headers: request.headers
    }
    if (['POST', 'PUT'].includes(request.method.toUpperCase())) {
        init.body = request.body
    }

    const response = await fetch(u, init)
    return new Response(response.body, response)
}


export default {
    async fetch(request, env, ctx) {
        const url = new URL(request.url);
        const path = url.pathname + url.search

        if (request.headers.has('x-real-host')) {
            const hosts = request.headers.get('x-real-host').trim().split(',')
            const host = hosts[(new Date()).valueOf() % hosts.length]
            const u = "https://" + host + path
            console.log("request url:", u)
            return await forward(request, u).catch(e => {})
        } else if (path.startsWith("/https://") || path.startsWith("/http://")) {
            const u = path.substring(1)
            console.log("request url:", u)
            try {
                return await forward(request, u)
            } catch (e) {
                return new Response(e.message, {status: 400})
            }
        }

        const upgradeHeader = request.headers.get('Upgrade');
        if (!upgradeHeader || upgradeHeader !== 'websocket') {
            switch (url.pathname) {
                case "/check":
                    return check(request)
                default:
                    return new Response("hello world");
            }
        } else if (url.pathname === "/api/ssh") {
            return await websocket(request).catch(e => {})
        }
    }
}
