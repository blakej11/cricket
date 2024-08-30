package main

import "fmt"
import "io"
import "net"
import "net/http"
import "strconv"
import "strings"
import "time"

// ServiceInfo holds the information about a cricket obtained from mDNS.
type ServiceInfo struct {
	ID      string
	Address net.IP
	Port    int
}

// cricket holds all of the per-cricket information available.
type cricket struct {
	ServiceInfo
	name		string		// for human-readable names
	creation	time.Time
	lastPing	time.Time
}

var crickets map[string]*cricket

var defaultVolume = 4

// --------------------------------------------------------------------

func MDNSListener(infos <-chan *ServiceInfo) {
	crickets = make(map[string]*cricket)
	for i := range infos {
		Infof("Cricket %q at %v:%d", i.ID, i.Address, i.Port)
		if _, ok := crickets[i.ID]; ok {
			Infof("Replacing existing cricket %v", i.ID)
		}
		crickets[i.ID] = newCricket(*i)
	}
}

// XXX this needs to be thread safe
func Player() {
	go func() {
		for {
			time.Sleep(15 * time.Second)
			for _, c := range crickets {
				_ = c.blink(2.0, 6)
			}
		}
	}()

	for {
		time.Sleep(24 * time.Second)
		for _, c := range crickets {
			_ = c.play(1, 1)
		}
	}
}

func newCricket(info ServiceInfo) *cricket {
	c := &cricket{
		ServiceInfo: info,
		name: "",
		creation: time.Now(),
	}

	go func(c *cricket) {
		// XXX
		time.Sleep(time.Second)
		_ = c.setVolume(defaultVolume)
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
	arg1 := fmt.Sprintf("folder=%d", folder);
	arg2 := fmt.Sprintf("file=%d", file);
	_, err := c.getURL("play", arg1, arg2);
	return err
}

func (c *cricket) setVolume(volume int) error {
	arg1 := fmt.Sprintf("volume=%d", volume);
	_, err := c.getURL("setvolume", arg1, "persist=true")
	return err
}

func (c *cricket) blink(speed float32, reps int) error {
	arg1 := fmt.Sprintf("speed=%f", speed);
	arg2 := fmt.Sprintf("reps=%d", reps);
	_, err := c.getURL("blink", arg1, arg2)
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
	// strings.TrimSpace()?
	v, err := strconv.ParseFloat(body, 32)
	if err != nil {
		return 0.0, err
	}
	return float32(v), nil
}

func (c *cricket) queue() (int, error) {
	body, err := c.getURL("queue")
	if err != nil {
		return 0, err
	}
	// strings.TrimSpace()?
	v, err := strconv.ParseInt(body, 10, 32)
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
