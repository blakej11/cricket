module github.com/blakej11/cricket

go 1.23

require github.com/libp2p/zeroconf/v2 v2.2.0

require (
	github.com/miekg/dns v1.1.43 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
)

replace github.com/libp2p/zeroconf/v2 => github.com/blakej11/zeroconf/v2 v2.2.0
