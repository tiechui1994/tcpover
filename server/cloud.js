// @ts-ignore
import {connect} from 'cloudflare:sockets';

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
    constructor(initialCapacity = 512) {
        this.data = new Uint8Array(initialCapacity);
    }

    /**
     * @param {Uint8Array} chunk 
     */
    write(chunk) {
        const needed = this.size + chunk.byteLength;
        if (needed > this.data.byteLength) {
            let newCapacity = this.data.byteLength * 2;
            if (newCapacity < needed) newCapacity = needed;
            info(`size to: ${newCapacity}`)
            const newData = new Uint8Array(newCapacity);
            newData.set(this.data);
            this.data = newData;
        }
        // 使用高效的底层内存拷贝
        this.data.set(chunk, this.size);
        this.size += chunk.byteLength;
    }

    /**
     * @return Uint8Array
     */
    bytes() { return this.data.subarray(0, this.size); }

    /**
     * @param {number} start 
     * @param {number} end 
     * @return Uint8Array
     */
    subarray(start, end) { return this.data.subarray(start, end ?? this.size);}
    /**
     * @param {number} index
     */
    at(index) { return this.data[index]; }
    length() { return this.size; }
    release() { this.size = 0; }
}

class WebSocketStream {
    constructor(socket) {
        this.socket = socket
        this.readable = new ReadableStream({
            start(controller) {
                socket.onmessage = (event) => {
                    controller.enqueue(new Uint8Array(event.data));
                };
                socket.onerror = (e) => {
                    warn("readable onerror:", e.message)
                    controller.error(e)
                }
            },
            pull(controller) {
            },
            cancel() {
                safeExecute(() => socket.close(1000, "readable cancel"));
            },
        });
        this.writable = new WritableStream({
            start(controller) {
                socket.onerror = (e) => warn("writable onerror:", e.message)
            },
            write(chunk, controller) {
                socket.send(chunk);
            },
            close() {
                safeExecute(() => socket.close());
            },
            abort(e) {
                safeExecute(() => socket.close(1006, "writable abort"));
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

/**
 * @param { () => any} fn 
 */
const safeExecute = (fn) => {
    try {
        const result = fn();
        if (result instanceof Promise) {
            result.catch(() => {});
        }
    } catch (_) {}
};

class MuxSocketStream {
    constructor(stream) {
        this.stream = stream;
        this.sessions = {}
        this.run().catch((err) => {
            warn("run::catch", err.message)
            safeExecute(() => this.stream.socket.close(1000));
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
         * @param {WebSocket} socket 
         */
        const handleRemoteToLocal = async(conn, id, socket) => {
            // 核心性能优化：预分配一个复用的发送缓冲区，避免每次发送都创建新数组
            const sendBuffer = new Uint8Array(8 + 4096);
            const view = new DataView(sendBuffer.buffer);
            
            // 预设头部固定不变的数据
            view.setUint8(0, 1);
            view.setUint8(1, PSH);
            view.setUint32(4, id, true);

            let first = true;
            try {
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
                        view.setUint16(2, size, true);

                        // 将数据拷贝到预分配的 Buffer 头部之后
                        sendBuffer.set(chunk.subarray(index, index + size), 8);
                        
                        // 一次性发送完整的 WebSocket 帧，合并 Header 与 Data
                        socket.send(sendBuffer.subarray(0, 8 + size));
                        
                        index += size;
                    }
                }
            }finally {
                safeExecute(() => conn.close());
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
                view.setUint16(2, 0, true)
                view.setUint32(4, id, true)

                safeExecute(() => socket.send(view.buffer));
                safeExecute(() => session.conn.close());
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
                    realLen  += 2
                    if (firstBuffer.length() < realLen) return

                    socket.send(new Uint8Array(2))

                    readRequest = false
                    readRequest = false;
                    chunk = firstBuffer.subarray(realLen);
                    firstBuffer.release();
                    if (chunk.byteLength === 0) return;
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

                    try {
                        info(`domain: ${hostname}:${port}`)
                        const conn = connect(`${hostname}:${port}`, {secureTransport: "off"})
                        const writer = conn.writable.getWriter()
                        if (chunk.byteLength > 0) {
                            await writer.write(chunk)
                        }

                        sessions[sessionID] = {conn: conn, writer: writer}
                        handleRemoteToLocal(conn, sessionID, socket).catch((err) => {
                            warn("connect::catch", err.message)
                            closeSession(sessionID)
                        }).then(() => {
                            closeSession(sessionID)
                        })
                    } catch (e) {
                        error("connect error", e.message);
                        closeSession(sessionID);
                    }
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
            warn("read write pipe exception", e.message);
        })
    }
}


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
    if (proto === protoVless) {
        port = buffer[20] | buffer[19] << 8
        const type = buffer[21];
        if (type == 1) {
            hostname = buffer.subarray(22, 22 + 4).join(".")
            remain = buffer.subarray(22 + 4)
        } else if (type == 2) {
            const length = buffer[22]
            hostname = decoder.decode(buffer.subarray(23, 23 + length))
            remain = buffer.subarray(23 + length)
        } else if (type == 3) {
            hostname = buffer.subarray(22, 22 + 16).join(":")
            remain = buffer.subarray(22 + 16)
        }
    } else {
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
    }

    return {hostname, port, remain}
}

async function websocket(request) {
    const url = new URL(request.url);

    const rule = url.searchParams.get("rule") || ""
    const mode = url.searchParams.get("mode") || ""

    if ([ruleConnector, ruleAgent].includes(rule) && [modeDirect, modeDirectMux].includes(mode)) {
        // @ts-ignore
        const webSocketPair = new WebSocketPair();
        const [client, socket] = Object.values(webSocketPair);
        socket.accept();

        const proto = request.headers.get("proto")
        if (modeDirect === mode) {
            socket.onmessage = async (event) => {
                const {hostname, port, remain} = parseProtoAddress(proto, new Uint8Array(event.data))
                if (proto === protoVless) {
                    socket.send(new Uint8Array(2))
                }

                try {
                    const local = new WebSocketStream(socket)

                    info(`real addr: ${hostname}:${port}`)
                    const remote = connect(`${hostname}:${port}`, {secureTransport: "off"})
                    if (remain && remain.byteLength > 0) {
                        const writer = remote.writable.getWriter()
                        await writer.write(remain)
                        writer.releaseLock();
                    }

                    const closeAll = () => {
                        try { socket.close() } catch(e) { }
                        try { remote.close() } catch(e) { }
                    }

                    Promise.all([
                        local.readable.pipeTo(remote.writable).catch((e) => warn("local pipe error:", e.message)),
                        remote.readable.pipeTo(local.writable).catch((e) => warn("remote pipe error:", e.message))
                    ]).finally(() => {
                        closeAll()
                    })
                } catch(e) {
                    warn("connect direct error:", e.message);
                    safeExecute(() => socket.close());
                } 
            }
        } else {
            new MuxSocketStream(new WebSocketStream(socket))
        }

        return new Response(null, {
            status: 101,
            webSocket: client,
        });
    }

    if (rule === ruleManage) {
        const webSocketPair = new WebSocketPair();
        const [client, socket] = Object.values(webSocketPair);
        socket.accept();

        socket.addEventListener("open", (event) => {
            new WebSocketStream(socket)
        });

        return new Response(null, {
            status: 101,
            webSocket: client,
        });
    }

    return new Response("Bad Request", {status: 400});
}

async function speed(request) {
    try {
        const upgrade = request.headers.get("upgrade") || "";
        if (upgrade.toLowerCase() != "websocket") {
            return new Response("request isn't trying to upgrade to websocket.", { status: 400});
        }

        const webSocketPair = new WebSocketPair();
        const [client, socket] = Object.values(webSocketPair);
        socket.accept();

        socket.onmessage = async (event) => {
            const packet = new Uint8Array(event.data)
            const type = packet[0]
            const len = packet[1]
            const address = decoder.decode(packet.subarray(2, 2+len))
            const tokens = address.split(":")
            const hostname = tokens[0].trim()
            const port = tokens.length >= 2 ? parseInt(tokens[1].trim()) : 443

            info(hostname, port, type == 0x01 ? "TCP" : "UDP")
            const local = new WebSocketStream(socket)

            if(type === 0x01) {
                const remote = connect(`${hostname}:${port}`, {secureTransport: "off"})
                const closeAll = () => {
                    try { socket.close() } catch(e) { }
                    try { remote.close() } catch(e) { }
                }
                    
                await Promise.all([
                    local.readable.pipeTo(remote.writable).catch((e) => {
                        warn("local exception:", e.message)
                    }),
                    remote.readable.pipeTo(local.writable).catch((e) => {
                        warn("remote exception:", e.message);
                    })
                ]).catch( (e) => {
                    warn("close:", e.message)
                }).finally(() => closeAll())
            } else {
                await socket.close(1000, "udp closed")
            }
        }

        return new Response(null, {
            status: 101,
            webSocket: client,
            headers: {
                'x-ip': request.headers.get('x-real-ip')
            }
        });
    } catch(e) {
        console.warn(`speed: ${e.message}`)        
    }
}


async function forward(request, u) {
    const url = new URL(u)
    request.headers['host'] = url.host
    return await fetch(u, request)
}


export default {
    async fetch(request, env, ctx) {
        const url = new URL(request.url);
        const path = url.pathname + url.search

        if (request.headers.has('x-real-host')) {
            const hosts = request.headers.get('x-real-host').trim().split(',')
            const host = hosts[(new Date()).valueOf() % hosts.length]
            const u = "https://" + host + path
            info("request url:", u)
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
            return new Response("hello world");
        } else if (url.pathname === "/api/ssh") {
            return await websocket(request)
        } else if (url.pathname === "/api/speed") {
            return await speed(request)
        }
    }
}
