package decorator

import "github.com/miekg/dns"

type Decorator interface {
	Wrap(dns.Handler) dns.Handler
}

type DecoratorFunc func(dns.Handler) dns.Handler

func (df DecoratorFunc) Wrap(f dns.Handler) dns.Handler {
	return df(f)
}

type Logger interface {
	Printf(format string, v ...interface{})
}

func Log(l Logger, opcode int) Decorator {
	return DecoratorFunc(func(h dns.Handler) dns.Handler {
		return dns.HandlerFunc(func(w dns.ResponseWriter, m *dns.Msg) {
			if m.Opcode == opcode {
				l.Printf("remoteAddr %s\n%v", w.RemoteAddr(), m)
			}

			h.ServeDNS(w, m)
		})
	})
}
