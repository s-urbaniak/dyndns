package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/boltdb/bolt"
	"github.com/miekg/dns"
)

var (
	tsig     *string
	db_path  *string
	port     *int
	bdb      *bolt.DB
	logfile  *string
	pid_file *string
)

var rr_bucket = []byte{'r', 'r'}

func newKey(domain string, rtype uint16) (string, error) {
	if n, ok := dns.IsDomainName(domain); ok {
		labels := dns.SplitDomainName(domain)

		// Reverse domain, starting from top-level domain
		for i := 0; i < n/2; i++ {
			j := n - i - 1
			labels[i], labels[j] = labels[j], labels[i]
		}

		reverse_domain := strings.Join(labels, ".")
		return strings.Join([]string{reverse_domain, strconv.Itoa(int(rtype))}, "_"), nil
	}

	return "", errors.New("Invalid domain: " + domain)
}

func createBucket(bucket []byte) (err error) {
	err = bdb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucket)
		return err
	})

	return err
}

func remove(domain string, rtype uint16) (err error) {
	key, _ := newKey(domain, rtype)
	err = bdb.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rr_bucket)
		return b.Delete([]byte(key))
	})

	return err
}

func store(domain string, rtype uint16, rrs []dns.RR) (err error) {
	key, _ := newKey(domain, rtype)

	err = bdb.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rr_bucket)
		var buf bytes.Buffer
		vs := make([]string, len(rrs))

		for i, _ := range rrs {
			vs[i] = rrs[i].String()
		}

		gob.NewEncoder(&buf).Encode(vs)
		return b.Put([]byte(key), buf.Bytes())
	})

	return err
}

func get(domain string, rtype uint16) ([]dns.RR, error) {
	key, _ := newKey(domain, rtype)
	var vs []string

	err := bdb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rr_bucket)
		vb := b.Get([]byte(key))

		if len(vb) == 0 {
			return errors.New("Record not found, key: " + key)
		}

		buf := bytes.NewBuffer(vb)
		gob.NewDecoder(buf).Decode(&vs)

		return nil
	})

	if err != nil {
		return nil, err
	}

	rrs := make([]dns.RR, len(vs))
	for i, _ := range vs {
		rr, err := dns.NewRR(vs[i])
		if err != nil {
			return nil, err
		}
		rrs[i] = rr
	}

	return rrs, nil
}

func update(r dns.RR, q *dns.Question) {
	var (
		rrs   []dns.RR
		name  string
		rtype uint16
		ttl   uint32
		ip    net.IP
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
		remove(name, rtype)
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
		rrs, _ = get(name, rtype)
		if rrs == nil {
			rrs = []dns.RR{}
		}

		ip = a.A
		rr := &dns.A{
			Hdr: rheader,
			A:   ip,
		}
		rrs = append(rrs, rr)

	case *dns.AAAA:
		rrs, _ = get(name, rtype)
		if rrs == nil {
			rrs = []dns.RR{}
		}

		ip = a.AAAA
		rr := &dns.AAAA{
			Hdr:  rheader,
			AAAA: ip,
		}
		rrs = append(rrs, rr)

	default:
		// unsupported record type, skip
		return
	}

	store(name, rtype, rrs)
}

func query(m *dns.Msg) {
	var rrs []dns.RR
	var err error

	for _, q := range m.Question {
		if rrs, err = get(q.Name, q.Qtype); err != nil {
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
	pid_file = flag.String("pid", "./go-dyndns.pid", "pid file location")

	flag.Parse()

	// Open db
	db, err := bolt.Open(*db_path, 0600,
		&bolt.Options{Timeout: 10 * time.Second})

	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	bdb = db

	// Create dns bucket if doesn't exist
	createBucket(rr_bucket)

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
