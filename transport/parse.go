package transport

import (
	"fmt"
	"github.com/tiechui1994/tcpover/ctx"
	"github.com/tiechui1994/tcpover/transport/common/structure"
	"github.com/tiechui1994/tcpover/transport/outbound"
)

func ParseProxy(mapping map[string]interface{}) (ctx.Proxy, error) {
	decoder := structure.NewDecoder(structure.Option{TagName: "proxy", WeaklyTypedInput: true, KeyReplacer: structure.DefaultKeyReplacer})
	proxyType, existType := mapping["type"].(string)
	if !existType {
		return nil, fmt.Errorf("missing type")
	}
	var (
		proxy ctx.Proxy
		err   error
	)
	switch proxyType {
	case ctx.Wless:
		muxOption := &outbound.WlessOption{}
		err = decoder.Decode(mapping, muxOption)
		if err != nil {
			break
		}
		proxy, err = outbound.NewWless(*muxOption)
	case ctx.Vless:
		muxOption := &outbound.VlessOption{}
		err = decoder.Decode(mapping, muxOption)
		if err != nil {
			break
		}
		proxy, err = outbound.NewVless(*muxOption)
	case ctx.Direct:
		proxy = outbound.NewDirect()
	default:
		return nil, fmt.Errorf("unsupport proxy type: %s", proxyType)
	}

	return proxy, err
}
