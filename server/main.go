package main

import "context"
import "strings"
import "time"

import zeroconf "github.com/libp2p/zeroconf/v2"

func mDNSResolver(infos chan<- *ServiceInfo) {
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
			infos <- &ServiceInfo{
				ID:      s[1],
				Address: entry.AddrIPv4[0],
				Port:    entry.Port,
			}
		}
	}(entries)

	ctx := context.Background()
	err := zeroconf.Browse(ctx, "_http._tcp", "local.", entries)
	if err != nil {
		Fatalf("failed to browse mDNS: %v", err.Error())
	}

	<-ctx.Done()
}

func main() {
	infos := make(chan *ServiceInfo)

	go mDNSResolver(infos)
	go MDNSListener(infos)

	go Player()

	time.Sleep(10000 * time.Second)
}
