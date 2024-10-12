module github.com/blakej11/basic-cricket

go 1.22.4

require github.com/libp2p/zeroconf/v2 v2.2.0

require (
	github.com/miekg/dns v1.1.43 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
)

replace github.com/libp2p/zeroconf/v2 => github.com/blakej11/zeroconf/v2 v2.2.0
