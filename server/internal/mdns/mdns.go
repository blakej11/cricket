package mdns

import (
	"context"
	"strings"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"

	zeroconf "github.com/libp2p/zeroconf/v2"
)

func Start() {
	go resolver()
}

func resolver() {
	entries := make(chan *zeroconf.ServiceEntry)

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			if len(entry.AddrIPv4) < 1 {
				continue
			}
			s := strings.Split(entry.Instance, " ")
			if len(s) < 2 || !strings.HasPrefix(s[0], "Cricket") {
				continue
			}
			id := types.ID(s[1])
			loc := types.NetLocation{
				Address: entry.AddrIPv4[0],
				Port:    entry.Port,
			}
			client.Add(id, loc)
		}
	}(entries)

	ctx := context.Background()
	err := zeroconf.Browse(ctx, "_http._tcp", "local.", entries)
	if err != nil {
		log.Fatalf("failed to browse mDNS: %v", err.Error())
	}
	<-ctx.Done()	// should not be reached
}
