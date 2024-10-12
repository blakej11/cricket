module github.com/blakej11/cricket

go 1.22.4

require github.com/libp2p/zeroconf/v2 v2.2.0

require (
	github.com/miekg/dns v1.1.43 // indirect
	golang.org/x/net v0.0.0-20210423184538-5f58ad60dda6 // indirect
	golang.org/x/sys v0.0.0-20210426080607-c94f62235c83 // indirect
)

replace github.com/libp2p/zeroconf/v2 => github.com/blakej11/zeroconf/v2 v2.2.0
