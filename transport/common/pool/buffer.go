package pool

import (
	"bytes"
	"sync"

	"github.com/tiechui1994/tcpover/transport/common/protobytes"
)

var (
	bufferPool      = sync.Pool{New: func() interface{} { return &bytes.Buffer{} }}
	bytesBufferPool = sync.Pool{New: func() interface{} { return &protobytes.BytesWriter{} }}
)

func GetBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

func PutBuffer(buf *bytes.Buffer) {
	buf.Reset()
	bufferPool.Put(buf)
}

func GetBytesBuffer() *protobytes.BytesWriter {
	return bytesBufferPool.Get().(*protobytes.BytesWriter)
}

func PutBytesBuffer(buf *protobytes.BytesWriter) {
	buf.Reset()
	bytesBufferPool.Put(buf)
}
