package rules

import (
	"strings"

	"github.com/tiechui1994/tcpover/ctx"
	"golang.org/x/net/idna"
)

type DomainSuffix struct {
	suffix string
	adapter string
}

func NewDomainSuffix(domain string,adapter string) *DomainSuffix {
	punycode, _ := idna.ToASCII(strings.ToLower(domain))
	return &DomainSuffix{
		suffix: punycode,
		adapter: adapter,
	}
}

func (d *DomainSuffix) Name() string {
	return RuleDomainSuffix
}

func (d *DomainSuffix) Match(meta *ctx.Metadata) (bool, string) {
	domain := meta.Host
	return strings.HasSuffix(domain, "."+d.suffix) || domain == d.suffix, d.adapter
}

func (d *DomainSuffix) Payload() string {
	return d.suffix
}
