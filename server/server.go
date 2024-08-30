package main

import "context"
import "fmt"
import "log"
import "net"
import "strings"
import "time"

import zeroconf "github.com/libp2p/zeroconf/v2"

type Device struct {
	ID      string
	Address net.IP
	Port    int
}

func mDNSResolver(messages chan<- *Device) {
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
			messages <- &Device{
				ID:      s[1],
				Address: entry.AddrIPv4[0],
				Port:    entry.Port,
			}
		}
	}(entries)

	ctx := context.Background()
	err := zeroconf.Browse(ctx, "_http._tcp", "local.", entries)
	if err != nil {
		log.Fatalln("failed to browse mDNS:", err.Error())
	}

	<-ctx.Done()
}

func cricketListener(deviceMessages <-chan *Device) {
	for m := range deviceMessages {
		fmt.Printf("Cricket %q at %v:%d\n", m.ID, m.Address, m.Port)
	}
}

func main() {
	devices := make(chan *Device)

	go mDNSResolver(devices)
	go cricketListener(devices)

	time.Sleep(10000 * time.Second)
}
