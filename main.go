package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/miekg/dns"
	"github.com/s-urbaniak/dyndns/records"
)

var (
	tsig    *string
	db_path *string
	port    *int
	repo    records.Records
	logfile *string
)

func update(r dns.RR, q *dns.Question) {
	var (
		name  string
		rtype uint16
		ttl   uint32
	)

	header := r.Header()
	name = header.Name
	rtype = header.Rrtype
	ttl = header.Ttl

	if _, ok := dns.IsDomainName(name); !ok {
		// invalid domain name, skip
		return
	}

	if header.Class == dns.ClassANY && header.Rdlength == 0 {
		_ = repo.Delete(name, rtype)
		return
	}

	rheader := dns.RR_Header{
		Name:   name,
		Rrtype: rtype,
		Class:  dns.ClassINET,
		Ttl:    ttl,
	}

	switch a := r.(type) {
	case *dns.A:
		_ = repo.Append(&dns.A{
			Hdr: rheader,
			A:   a.A,
		})

	case *dns.AAAA:
		_ = repo.Append(&dns.A{
			Hdr: rheader,
			A:   a.AAAA,
		})

	default:
		// unsupported record type, skip
		return
	}
}

func query(m *dns.Msg) {
	var rrs []dns.RR
	var err error

	for _, q := range m.Question {
		if rrs, err = repo.Get(q.Name, q.Qtype); err != nil {
			// skip faulty records
			continue
		}

		for i, _ := range rrs {
			if rrs[i].Header().Name == q.Name {
				m.Answer = append(m.Answer, rrs[i])
			}
		}
	}
}

func handleDnsRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	switch r.Opcode {
	case dns.OpcodeQuery:
		query(m)

	case dns.OpcodeUpdate:
		for _, question := range r.Question {
			for _, rr := range r.Ns {
				log.Println("updating rr", rr)
				update(rr, &question)
			}
		}

	default:
		// unsupported opcode
		return
	}

	if w.TsigStatus() != nil {
		// invalid TSIG, ignore
		return
	}

	if r.IsTsig() != nil {
		m.SetTsig(
			r.Extra[len(r.Extra)-1].(*dns.TSIG).Hdr.Name,
			dns.HmacMD5,
			300,
			time.Now().Unix(),
		)
	}

	w.WriteMsg(m)
}

func serve(name, secret string, port int) {
	server := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}

	if name != " " {
		server.TsigSecret = map[string]string{name: secret}
	}

	err := server.ListenAndServe()
	defer server.Shutdown()

	if err != nil {
		log.Fatalf("Failed to setup the udp server: %s\n", err.Error())
	}
}

func main() {
	var (
		name   string // tsig keyname
		secret string // tsig base64
	)

	// Parse flags
	logfile = flag.String("logfile", " ", "path to log file")
	port = flag.Int("port", 53, "server port")
	tsig = flag.String("tsig", " ", "use MD5 hmac tsig: keyname:base64")
	db_path = flag.String("db_path", "./dyndns.db", "location where db will be stored")

	flag.Parse()

	// Open db
	var err error
	repo, err = records.OpenBolt(*db_path)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()

	// Attach request handler func
	dns.HandleFunc(".", handleDnsRequest)

	// Tsig extract
	if *tsig != " " {
		a := strings.SplitN(*tsig, ":", 2)
		name, secret = dns.Fqdn(a[0]), a[1]
	}

	// Start server
	go serve(name, secret, *port)

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case s := <-sig:
			log.Printf("Signal (%d) received, stopping\n", s)
			return
		}
	}
}
