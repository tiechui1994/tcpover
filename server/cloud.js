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

const debug = false
const warn = (...data) => {
    if (debug) console.warn(data)
}
const error = (...data) => {
    if (debug) console.error(data)
}
const info =  (...data) => {
    if (debug) console.info(data)
}

class Buffer {
    constructor(initialCapacity = 256) {
        this.data = new Uint8Array(initialCapacity);
    }

    /**
     * @param {chunk} chunk 
     */
    write(chunk) {
        const needed = this.size + chunk.byteLength;
        if (needed > this.data.byteLength) {
            this.grow(needed);
        }
        // 使用高效的底层内存拷贝
        this.data.set(chunk, this.size);
        this.size += chunk.byteLength;
    }

    grow(minCapacity) {
        info(`size to: ${this.data.byteLength * 2}`)
        let newCapacity = this.data.byteLength * 2;
        if (newCapacity < minCapacity) newCapacity = minCapacity;

        const newData = new Uint8Array(newCapacity);
        newData.set(this.data); // 拷贝原有数据
        this.data = newData;
    }

    /**
     * @return Uint8Array
     */
    bytes() {
        return this.data.subarray(0, this.size);
    }

    /**
     * @param {number} start 
     * @param {number} end 
     * @return Uint8Array
     */
    subarray(start, end) {
        return this.data.subarray(start, end ?? this.size);
    }
    /**
     * @param {number} index
     */
    at(index) { return this.data[index]; }
    length() { return this.size; }
    reset() { this.size = 0; }
    release() {
        this.size = 0
        this.data = null;
        info(`release`)
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
                    warn("<readable onerror>", e.message)
                    controller.error(e)
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
                    warn("<writable onerror>:", e.message)
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

const SYN = 0x00
const FIN = 0x01
const PSH = 0x02
const NOP = 0x03
const vlessHeaderLen = 1 + 16 + 1 + 1 + 2 + 1
const muxHeaderLen = 8

const decoder = new TextDecoder()

class MuxSocketStream {
    constructor(stream) {
        this.stream = stream;
        this.sessions = {}
        this.run().catch((err) => {
            warn("run::catch", err.stack)
            this.stream.socket.close(1000)
        })
    }

    async run() {
        const socket = this.stream.socket
        const sessions = this.sessions
        /**
         * @param {Uint8Array} a 
         * @param {Uint8Array} b 
         * @returns {Uint8Array}
         */
        const merge = (a, b) => {
            const res = new Uint8Array(a.byteLength + b.byteLength);
            res.set(a, 0);
            res.set(b, a.byteLength);
            return res;
        }
        /**
         * @param {Uint8Array} chunk 
         * @param {DataView} view 
         * @returns 
         */
        const parseMuxAddress = (chunk, view) => {
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

        /**
         * @param {any} conn 
         * @param {number} id 
         * @param {EmendWebsocket} socket 
         */
        const handleRemoteToLocal = async(conn, id, socket) => {
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
                    const size = Math.min(chunk.byteLength - index, 4096);
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
        /**
         * @param {number} id 
         */
        const closeSession = (id) => {
            const session = sessions[id];
            if (session) {
                const headerBuf = new Uint8Array(8);
                const view = new DataView(headerBuf.buffer);
                view.setUint8(0,1)
                view.setUint8(1, FIN)
                view.setUint16(2, 0,true)
                view.setUint32(4, id,true)
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

        const firstBuffer = new Buffer()
        let readRequest = true
        let remainChunk = null

        const writableStream = new WritableStream({
            start(controller) {
            },
            async write(chunk, controller) {
                if (readRequest) {
                    firstBuffer.write(chunk)
                    // 1) vless request
                    if (firstBuffer.length() < vlessHeaderLen) return

                    let realLen = vlessHeaderLen
                    const addrType = firstBuffer.at(vlessHeaderLen - 1)
                    if (addrType == 1) realLen+=4;
                    else if (addrType==3) realLen+=16;
                    else if (addrType==2) realLen += 1 + firstBuffer.at(vlessHeaderLen);

                    // 2) mux version info, 2 bytes
                    if (firstBuffer.length() < realLen + 2) return

                    socket.send(new Uint8Array(2))

                    realLen  += 2
                    readRequest = false
                    chunk = firstBuffer.subarray(realLen)
                    if (chunk.byteLength === 0) {
                        firstBuffer.release()
                        return
                    }
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

                    info(`domain: ${hostname}:${port}`)
                    const conn = connect(`${hostname}:${port}`, {secureTransport: "off"})
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
}

let uuid = null;

const ruleManage = "manager"
const ruleAgent = "Agent"
const ruleConnector = "Connector"

const modeDirect = "direct"
const modeForward = "forward"
const modeDirectMux = "directMux"
const modeForwardMux = "forwardMux"

const protoWless = "Wless"
const protoVless = "Vless"

/**
 * @param {string} proto 
 * @param {Uint8Array} buffer 
 * @returns 
 */
function parseProtoAddress(proto, buffer) {
    let port, hostname, remain
    switch (proto) {
        case protoVless:
            port = buffer[20] | buffer[19] << 8
            switch (buffer[21]) {
                case 1:
                    hostname = buffer.subarray(22, 22 + 4).join(".")
                    remain = buffer.subarray(22 + 4)
                    break
                case 3:
                    hostname = buffer.subarray(22, 22 + 16).join(":")
                    remain = buffer.subarray(22 + 16)
                    break
                case 2:
                    const length = buffer[22]
                    hostname = decoder.decode(buffer.subarray(23, 23 + length))
                    remain = buffer.subarray(23 + length)
                    break
            }
            break
        default:
            const len = buffer[0]
            const address = decoder.decode(buffer.subarray(1, 1 + len))
            const tokens = address.split(":")
            if (tokens.length >= 2) {
                hostname = tokens[0].trim()
                port = parseInt(tokens[1].trim())
            } else {
                hostname = tokens[0].trim()
                port = 443
            }
            remain = buffer.subarray(1 + len)
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
        warn('safeCloseWebSocket error', error);
    }
}

async function websocket(request) {
    const url = new URL(request.url);

    const name = url.searchParams.get("name") || ""
    const rule = url.searchParams.get("rule") || ""
    const mode = url.searchParams.get("mode") || ""

    if ([ruleConnector, ruleAgent].includes(rule) && [modeDirect, modeDirectMux].includes(mode)) {
        // @ts-ignore
        const webSocketPair = new WebSocketPair();
        const [client, webSocket] = Object.values(webSocketPair);
        webSocket.accept();

        const proto = request.headers.get("proto")
        if (modeDirect === mode) {
            webSocket.onmessage = async (event) => {
                const {hostname, port, remain} = parseProtoAddress(proto, new Uint8Array(event.data))
                if (proto === protoVless) {
                    webSocket.send(new Uint8Array(2))
                }

                const remote = new WebSocketStream(new EmendWebsocket(webSocket, `direct`))
                info(`real addr: ${hostname}:${port}, ${remain.byteLength}`)
                const local = connect(`${hostname}:${port}`, {secureTransport: "off"})
                if (remain && remain.byteLength > 0) {
                    const writer = local.writable.getWriter()
                    await writer.write(remain)
                    writer.releaseLock();
                }
                remote.readable.pipeTo(local.writable).catch((e) => {
                    warn("socket exception", e.message)
                    safeCloseWebSocket(webSocket)
                })
                local.readable.pipeTo(remote.writable).catch((e) => {
                    warn("socket exception", e.message)
                    safeCloseWebSocket(webSocket)
                })
            }
        } else {
            new MuxSocketStream(new WebSocketStream(new EmendWebsocket(webSocket, `mux-direct`)))
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
