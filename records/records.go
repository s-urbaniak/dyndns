package records

import (
	"io"

	"github.com/miekg/dns"
)

type Records interface {
	io.Closer

	Append(dns.RR) error
	Get(domain string, rtype uint16) ([]dns.RR, error)
	Delete(domain string, rtype uint16) error
}
