package records

import (
	"bytes"
	"encoding/gob"
	"strconv"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/miekg/dns"
	"github.com/pkg/errors"
)

type boltDB struct {
	db *bolt.DB
}

var rr_bucket = []byte{'r', 'r'}

func OpenBolt(path string) (*boltDB, error) {
	db, err := bolt.Open(
		path,
		0600,
		&bolt.Options{Timeout: 10 * time.Second},
	)

	if err != nil {
		return nil, errors.Wrap(err, "opening database failed")
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(rr_bucket)
		return err
	})

	if err != nil {
		return nil, errors.Wrap(err, "creating bucket failed")
	}

	return &boltDB{
		db: db,
	}, nil
}

func (b *boltDB) Append(rr dns.RR) error {
	key, err := newKey(rr.Header().Name, rr.Header().Rrtype)
	if err != nil {
		return errors.Wrap(err, "invalid key")
	}

	err = b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rr_bucket)
		rrs, err := get(b, key)
		if err != nil {
			return errors.Wrap(err, "get failed")
		}

		rrs = append(rrs, rr)
		return errors.Wrap(store(b, key, rrs), "store failed")
	})

	return errors.Wrapf(err, "key %s: append failed", key)
}

func (b *boltDB) Get(domain string, rtype uint16) ([]dns.RR, error) {
	key, err := newKey(domain, rtype)
	if err != nil {
		return nil, errors.Wrap(err, "invalid key")
	}

	var rrs []dns.RR
	err = b.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(rr_bucket)
		var err error
		rrs, err = get(b, key)
		return errors.Wrap(err, "get failed")
	})

	return rrs, errors.Wrapf(err, "key %s: view failed", key)
}

func (b *boltDB) Delete(domain string, rtype uint16) error {
	key, err := newKey(domain, rtype)
	if err != nil {
		return errors.Wrap(err, "invalid key")
	}

	return b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(rr_bucket)
		return errors.Wrap(b.Delete([]byte(key)), "delete failed")
	})
}

func (b *boltDB) Close() error {
	return b.db.Close()
}

func newKey(domain string, rtype uint16) (string, error) {
	n, ok := dns.IsDomainName(domain)
	if !ok {
		return "", errors.Errorf("invalid domain: %s", domain)
	}

	labels := dns.SplitDomainName(domain)

	// Reverse domain, starting from top-level domain
	for i := 0; i < n/2; i++ {
		j := n - i - 1
		labels[i], labels[j] = labels[j], labels[i]
	}

	reverse_domain := strings.Join(labels, ".")
	return strings.Join([]string{reverse_domain, strconv.Itoa(int(rtype))}, "_"), nil

}

func get(b *bolt.Bucket, key string) ([]dns.RR, error) {
	vb := b.Get([]byte(key))

	if len(vb) == 0 {
		return []dns.RR{}, nil
	}

	var vs []string
	buf := bytes.NewBuffer(vb)
	if err := gob.NewDecoder(buf).Decode(&vs); err != nil {
		return nil, errors.Wrap(err, "decoding failed")
	}

	rrs := make([]dns.RR, len(vs))
	for i, _ := range vs {
		rr, err := dns.NewRR(vs[i])
		if err != nil {
			return nil, errors.Wrap(err, "invalid RR")
		}
		rrs[i] = rr
	}

	return rrs, nil
}

func store(b *bolt.Bucket, key string, rrs []dns.RR) (err error) {
	var buf bytes.Buffer
	vs := make([]string, len(rrs))

	for i, _ := range rrs {
		vs[i] = rrs[i].String()
	}

	if err := gob.NewEncoder(&buf).Encode(vs); err != nil {
		return errors.Wrap(err, "encoding failed")
	}

	return errors.Wrap(
		b.Put([]byte(key), buf.Bytes()),
		"put failed",
	)
}
