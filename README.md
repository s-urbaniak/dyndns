## Introduction

This is a small test DNS server which understands

- Dynamic DNS Updates [RFC2136](https://tools.ietf.org/html/rfc2136)
- Secret key based transaction authentication [RFC2845](https://tools.ietf.org/html/rfc2845)
- A and AAAA record queries

## Quickstart
### Starting dyndns
First, generate a base64 encoded "secret"
```
$ echo 'secret' | base64
c2VjcmV0Cg==
```

Start the DNS server
```
$ sudo dyndns -tsig=k8s.:c2VjcmV0Cg== -port=53
```

### Create records using nsupdate
Generate a nsupdate input file
```
$ cat update.txt
server localhost 53
debug yes
key k8s. c2VjcmV0Cg==
zone master.
update delete master.k8s. A
update add master.k8s. 120 A 192.168.2.1
update add master.k8s. 120 A 192.168.2.2
show
send
```

Invoke nsupdate
```
$ nsupdate update.txt 
04-Jul-2017 18:02:10.264 the key 'k8s' is too short to be secure
Outgoing update query:
;; ->>HEADER<<- opcode: UPDATE, status: NOERROR, id:      0
;; flags:; ZONE: 0, PREREQ: 0, UPDATE: 0, ADDITIONAL: 0
;; ZONE SECTION:
;master.				IN	SOA

;; UPDATE SECTION:
master.k8s.		0	ANY	A	
master.k8s.		120	IN	A	192.168.2.1
master.k8s.		120	IN	A	192.168.2.2

Sending update to ::1#53
Outgoing update query:
;; ->>HEADER<<- opcode: UPDATE, status: NOERROR, id:  42585
;; flags:; ZONE: 1, PREREQ: 0, UPDATE: 3, ADDITIONAL: 1
;; ZONE SECTION:
;master.				IN	SOA

;; UPDATE SECTION:
master.k8s.		0	ANY	A	
master.k8s.		120	IN	A	192.168.2.1
master.k8s.		120	IN	A	192.168.2.2

;; TSIG PSEUDOSECTION:
k8s.			0	ANY	TSIG	hmac-md5.sig-alg.reg.int. 1499184130 300 16 O0stBiCDFx5ZO7YISfmYgw== 42585 NOERROR 0 


Reply from update query:
;; ->>HEADER<<- opcode: UPDATE, status: NOERROR, id:  42585
;; flags: qr; ZONE: 1, PREREQ: 0, UPDATE: 0, ADDITIONAL: 1
;; ZONE SECTION:
;master.				IN	SOA

;; TSIG PSEUDOSECTION:
k8s.			0	ANY	TSIG	hmac-md5.sig-alg.reg.int. 1499184130 300 16 +qqZDYXbYMABWt330qYFsA== 42585 NOERROR 0 
```

### Create records using Terraform
```
$ cat main.tf
provider "dns" {
  update {
    server        = "127.0.0.1"
    key_name      = "k8s."
    key_algorithm = "hmac-md5"
    key_secret    = "c2VjcmV0Cg=="
  }
}

resource "dns_a_record_set" "www" {
  zone = "k8s."
  name = "master"

  addresses = [
    "192.168.2.1",
    "192.168.2.2",
  ]

  ttl = 300
}
```

Invoke terraform
```
$ terraform apply
dns_a_record_set.www: Creating...
  addresses.#:          "" => "2"
  addresses.2315472134: "" => "192.168.2.1"
  addresses.319429820:  "" => "192.168.2.2"
  name:                 "" => "master"
  ttl:                  "" => "300"
  zone:                 "" => "k8s."
dns_a_record_set.www: Creation complete (ID: master.k8s.)

Apply complete! Resources: 1 added, 0 changed, 0 destroyed.

The state of your infrastructure has been saved to the path
below. This state is required to modify and destroy your
infrastructure, so keep it safe. To inspect the complete state
use the `terraform show` command.

State path: 
```

### Querying records
```
$ dig +short a @127.0.0.53 master.k8s
192.168.2.1
192.168.2.2
```
