package types

import (
	"net"
)

// These are the types that don't belong anywhere else.

// NetLocation holds the information about how to contact a client.
type NetLocation struct {
        Address		net.IP
        Port		int
}

// ID is the main way that clients are referred to.
type ID string

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

