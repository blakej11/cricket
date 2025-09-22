package builtinvc

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

        "github.com/blakej11/cricket/internal/fileset"
	_ "github.com/blakej11/cricket/internal/light"
        "github.com/blakej11/cricket/internal/log"
	_ "github.com/blakej11/cricket/internal/sound"
        "github.com/blakej11/cricket/internal/types"
)

// ---------------------------------------------------------------------

// builtinMockServer can simulate a cricket client.
type builtinMockServer struct {
	loc		*types.NetLocation
	files		map[int]map[int]float64
	requests	map[string]map[types.LeaseType][]time.Time
}

const (
	bmsHost = "127.0.0.1"
	bmsPort = 8080
)

func Start(fileSets map[string]*fileset.Set) (*types.NetLocation, error) {
	bms := builtinMockServer{
		loc: &types.NetLocation{
			Address: net.ParseIP(bmsHost),
			Port: bmsPort,
		},
		files: parseFilesets(fileSets),
		requests: make(map[string]map[types.LeaseType][]time.Time),
	}

	go bms.start()
	return bms.loc, nil
}

func parseFilesets(sets map[string]*fileset.Set) map[int]map[int]float64 {
	folders := make(map[int]map[int]float64)
	for _, s := range sets {
		for _, f := range s.Set() {
			if folders[f.Folder] == nil {
				folders[f.Folder] = make(map[int]float64)
			}
			folders[f.Folder][f.File] = f.Duration
		}
	}
	return folders
}

func (bms *builtinMockServer) start() {
	http.HandleFunc("/", func (w http.ResponseWriter, r *http.Request) {
		bms.handle(w, r)
	})
	addr := fmt.Sprintf("%s:%d", bmsHost, bmsPort)
	log.Infof("virtual cricket server running on %s", addr)
	http.ListenAndServe(addr, nil)
}

func (bms *builtinMockServer) handle(w http.ResponseWriter, r *http.Request) {
	fail := func (w http.ResponseWriter, s string) {
		w.WriteHeader(400)
		w.Write([]byte(s))
	}

	args, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		fail(w, fmt.Sprintf("failed to parse: %v\n", err))
		return
	}
	cricket := args.Get("cricketID")
	if cricket == "" {
		fail(w, "no cricket specified")
		return
	}
	args.Del("cricketID")
	if _, ok := bms.requests[cricket]; !ok {
		bms.requests[cricket] = make(map[types.LeaseType][]time.Time)
		bms.requests[cricket][types.Sound] = nil
		bms.requests[cricket][types.Light] = nil
	}

	desc := ""
	switch r.URL.Path {
	case "/play":
		desc, err = bms.play(cricket, args)
		if err != nil {
			fail(w, fmt.Sprintf("%w", err))
		}
	case "/blink":
		desc, err = bms.blink(cricket, args)
		if err != nil {
			fail(w, fmt.Sprintf("%w", err))
		}
	case "/soundpending":
		fmt.Fprintf(w, "%d", bms.pending(cricket, types.Sound))
	case "/lightpending":
		fmt.Fprintf(w, "%d", bms.pending(cricket, types.Light))
	case "/battery":
		fmt.Fprintf(w, "4.20")  // nice
	case "/setvolume":  // do nothing
	case "/ping":  // do nothing
	case "/pause":  // do nothing
	case "/unpause":  // do nothing
	case "/stop":  // do nothing
	default:
		fail(w, fmt.Sprintf("can't handle path %q\n", r.URL.Path))
	}

	if desc != "" {
		desc = fmt.Sprintf(" (%s)", desc)
	}
	if err != nil {
		desc = fmt.Sprintf("%s (failed: %v)", desc, err)
	}
	log.Debugf("vc: %s: got %s, args %v%s\n", cricket, r.URL.Path, args, desc)
}

func (bms *builtinMockServer) play(cricket string, args url.Values) (string, error) {
	folder, err := strconv.ParseInt(args.Get("folder"), 10, 32)
	if err != nil {
		return "", fmt.Errorf("can't find folder %q", folder)
	}
	if _, ok := bms.files[int(folder)]; !ok {
		return "", fmt.Errorf("can't find folder %q", folder)
	}

	file, err := strconv.ParseInt(args.Get("file"), 10, 32)
	if err != nil {
		return "", fmt.Errorf("can't find file %q", file)
	}
	if _, ok := bms.files[int(folder)][int(file)]; !ok {
		return "", fmt.Errorf("can't find file %q", file)
	}
	dur := bms.files[int(folder)][int(file)]
	fullDur := dur

	reps, _ := strconv.ParseFloat(args.Get("reps"), 64)
	delay, _ := strconv.ParseFloat(args.Get("delay"), 64)
	if reps > 0.0 {
		fullDur = dur * reps + (delay / 1000.0) * (reps - 1)
	}
	fullDur += 0.5

	remaining := bms.enqueue(cricket, types.Sound, dur)
	desc := fmt.Sprintf("%.3f -> %.3f sec (was %.3f)", dur, fullDur, remaining.Seconds())
	return desc, nil
}

func (bms *builtinMockServer) blink(cricket string, args url.Values) (string, error) {
	speed, _ := strconv.ParseFloat(args.Get("speed"), 64)
	reps, _ := strconv.ParseFloat(args.Get("reps"), 64)
	delay, _ := strconv.ParseFloat(args.Get("delay"), 64)
	if (speed < 0.001) {
		return "", fmt.Errorf("speed must be faster")
	} else if (reps <= 0) {
		return "", fmt.Errorf("reps must be a positive number");
	}
	dur := (256.0 / speed) * 2
	fullDur := dur
	if reps > 0 {
		fullDur = dur * reps + delay * (reps - 1)
	}
	dur /= 1000.0
	fullDur /= 1000.0

	remaining := bms.enqueue(cricket, types.Light, dur)
	desc := fmt.Sprintf("%.3f -> %.3f sec (was %.3f)", dur, fullDur, remaining.Seconds())
	return desc, nil
}

func (bms *builtinMockServer) enqueue(cricket string, t types.LeaseType, dur float64) time.Duration {
	endTime := time.Now()
	if queue := bms.requests[cricket][t]; queue != nil {
		endTime = queue[len(queue) - 1]
	}
	remaining := time.Now().Sub(endTime)
	endTime = endTime.Add(time.Duration(dur * float64(time.Second)))
	bms.requests[cricket][t] = append(bms.requests[cricket][t], endTime)
	return remaining
}

func (bms *builtinMockServer) pending(cricket string, t types.LeaseType) int {
	queue := bms.requests[cricket][t]
	for queue != nil {
		if time.Now().Before(queue[0]) {
			break
		}

		if len(queue) > 1 {
			queue = queue[1:]
		} else {
			queue = nil
		}
	}
	bms.requests[cricket][t] = queue
	return len(queue)
}
