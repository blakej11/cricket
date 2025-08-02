package types

import (
	"fmt"
	"net"
)

// These are the types that don't belong anywhere else.

// NetLocation holds the information about how to contact a client.
type NetLocation struct {
        Address		net.IP
        Port		int
}

func (n NetLocation) Equal(o NetLocation) bool {
	return n.Address.Equal(o.Address) && n.Port == o.Port
}

func (n NetLocation) String() string {
	return fmt.Sprintf("%s:%d", n.Address, n.Port)
}

// Client describes configuration parameters for a single client.
type Client struct {
	// A more familiar name for the client.
	Name		string

	// Where the client is located physically.
	PhysLocation
}

type PhysLocation struct {
	// Nothing right now.
}
