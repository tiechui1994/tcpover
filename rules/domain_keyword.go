package rules

import (
	"strings"

	"github.com/tiechui1994/tcpover/ctx"
	"golang.org/x/net/idna"
)

type DomainKeyword struct {
	keyword string
	adapter string
}

func NewDomainKeyword(domain string, adapter string) *DomainKeyword {
	punycode, _ := idna.ToASCII(strings.ToLower(domain))
	return &DomainKeyword{
		keyword:  punycode,
		adapter: adapter,
	}
}

func (d *DomainKeyword) Name() string  {
	return RuleDomainKeyword
}

func (d *DomainKeyword) Match(meta *ctx.Metadata) (bool,string)  {
	return strings.Contains(meta.Host, d.keyword), d.adapter
}

func (d *DomainKeyword) Payload() string  {
	return d.keyword
}


