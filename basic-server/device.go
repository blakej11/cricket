package main

import "fmt"
import "io"
import "net"
import "net/http"
import "strconv"
import "strings"
import "time"

// NetLocation holds the information about a cricket obtained from mDNS.
type NetLocation struct {
	ID      string
	Address net.IP
	Port    int
}

// cricket holds all of the per-cricket information available.
type cricket struct {
	NetLocation
	name		string		// for human-readable names
	creation	time.Time
	lastPing	time.Time

	targetVolume	int
}

var crickets map[string]*cricket

// --------------------------------------------------------------------

// XXX this needs to be thread safe w/r/t CricketListener
func Player() {
        go func() {
                for {
                        time.Sleep(10 * time.Second)
                        for _, c := range crickets { _ = c.blink(2.0, 100, 50, 4) }
                }
        }()

        go func() {
                for {
                        time.Sleep(11 * time.Second)
                        for _, c := range crickets { _, _ = c.battery() }
                }
        }()

        for {
                time.Sleep(15 * time.Second)
                for _, c := range crickets { _ = c.play(1, 1) }
        }
}

// --------------------------------------------------------------------

var defaultVolume = 24

func CricketListener(locs <-chan *NetLocation) {
	crickets = make(map[string]*cricket)
	for l := range locs {
		Infof("Cricket %q at %v:%d", l.ID, l.Address, l.Port)
		if _, ok := crickets[l.ID]; ok {
			Infof("Replacing existing cricket %v", l.ID)
		}
		crickets[l.ID] = newCricket(*l, defaultVolume)
	}
}

func newCricket(loc NetLocation, targetVolume int) *cricket {
	c := &cricket{
		NetLocation: loc,
		name: "",
		creation: time.Now(),
	}

	go func(c *cricket) {
		// XXX
		time.Sleep(time.Second)
		_ = c.setVolume(targetVolume)
	}(c)

	return c
}

func (c *cricket) ping() error {
	_, err := c.getURL("ping")
	if err != nil {
		return err
	}
	c.lastPing = time.Now()
	return nil
}

func (c *cricket) play(folder, file int) error {
	msg, err := c.getURL("play",
		fmt.Sprintf("folder=%d", folder),
		fmt.Sprintf("file=%d", file))
	if err != nil {
		return err
	}

	res := strings.Split(msg, ":")
	if len(res) == 2 {
		volume, err := strconv.Atoi(strings.TrimSpace(res[1]))
		// This can happen if a device resets.
		if err == nil && volume != c.targetVolume {
			c.setVolume(c.targetVolume)
		}
	}
	return nil
}

func (c *cricket) setVolume(volume int) error {
	arg1 := fmt.Sprintf("volume=%d", volume)
	_, err := c.getURL("setvolume", arg1, "persist=true")
	c.targetVolume = volume

	if err != nil {
		return err
	}

	return nil
}

func (c *cricket) blink(speed float32, delay, jitter, reps int) error {
	_, err := c.getURL("blink",
		fmt.Sprintf("speed=%f", speed),
		fmt.Sprintf("delay=%d", delay),
		fmt.Sprintf("jitter=%d", jitter),
		fmt.Sprintf("reps=%d", reps))
	return err
}

func (c *cricket) pause() error {
	_, err := c.getURL("pause")
	return err
}

func (c *cricket) unpause() error {
	_, err := c.getURL("unpause")
	return err
}

func (c *cricket) stop() error {
	_, err := c.getURL("stop")
	return err
}

func (c *cricket) battery() (float32, error) {
	body, err := c.getURL("battery")
	if err != nil {
		return 0.0, err
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(body), 32)
	if err != nil {
		return 0.0, err
	}
	return float32(v), nil
}

func (c *cricket) soundpending() (int, error) {
	body, err := c.getURL("soundpending")
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(strings.TrimSpace(body), 10, 32)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

func (c *cricket) lightpending() (int, error) {
	body, err := c.getURL("lightpending")
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(strings.TrimSpace(body), 10, 32)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

func (c *cricket) getURL(command string, args ...string) (string, error) {
	url := fmt.Sprintf("http://%s:%d/%s", c.Address, c.Port, command)
	urlArgs := strings.Join(args, "&")
	if urlArgs != "" {
		url = url + "?" + urlArgs
	}

	desc := fmt.Sprintf("%q", command)
	descArgs := strings.Join(args, ",")
	if descArgs != "" {
		desc = desc + " (" + descArgs + ")"
	}

	resp, err := http.Get(url)
	if err != nil {
		Errorf("[%s] %s returned error: %v", c.ID, desc, err)
		return "", err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Errorf("[%s] error while reading body from %s: %v", c.ID, desc, err)
		return "", err
	}

	if resp.StatusCode > 299 {
		Errorf("[%s] got failure status code from %s: %s", c.ID, desc, body)
		return "", fmt.Errorf("%s got status %d", desc, resp.StatusCode)
	}

	Infof("[%s] %s returned success: %s", c.ID, desc, body)
	return string(body), nil
}
