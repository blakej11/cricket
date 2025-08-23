package fileset

import (
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"time"
	"strings"
)

// Config describes a set of files that are operated on together.
type Config struct {
	Regex		string	// matches key in file map
}

// ---------------------------------------------------------------------
// Set is the runtime instantiation of a file set.

type Set struct {
	name	string
	files	[]File
}

func New(name string, c Config, files map[string]File) (*Set, error) {
	re, err := regexp.Compile(c.Regex)
	if err != nil {
		return nil, fmt.Errorf("failed to compile fileset %q regex %q: %w", name, c.Regex, err)
	}

	results := []File{}
	for name, file := range files {
		if re.MatchString(name) {
			results = append(results, file)
		}
	}
	return &Set{name: name, files: results}, nil
}

func (fs *Set) Set() []File {
	return fs.files
}

func (fs Set) String() string {
	results := []string{}
	for _, file := range fs.files {
		results = append(results, file.String())
	}
	return fmt.Sprintf("%s (%s)", fs.name, strings.Join(results, ", "))
}

func (fs Set) AverageDuration() time.Duration {
	total := 0.0
	for _, f := range fs.files {
		total += f.Duration
	}
	average := total / float64(len(fs.files))
	return time.Duration(float64(time.Second) * average)
}

func (fs *Set) Pick() File {
	return pick(fs.files)
}

func pick(files []File) File {
	return files[rand.Int32N(int32(len(files)))]
}

func (fs *Set) PickCarefully(deadline time.Time, reps int, delay, jitter time.Duration) Play {
	if reps == 0 {
		reps = 1
	}

	file := fs.Pick()
	actualReps := min(reps, findReps(file, deadline, delay))
	if actualReps == 0 {
		remaining := max(deadline.Sub(time.Now()).Seconds(), 0.0)
		var files []File
		for _, f := range fs.files {
			if f.Duration < remaining {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			return Play{}
		}
		file = pick(files)
		actualReps := min(reps, findReps(file, deadline, delay))
		if actualReps == 0 {
			return Play{}
		}
	}

	return Play{
		File:   file,
		Reps:   actualReps,
		Delay:	delay,
		Jitter:	jitter,
	}
}

// How many reps could this file play before the deadline,
// with the specified delay between each play?
func findReps(file File, deadline time.Time, delay time.Duration) int {
	// delay.Seconds() is added to "remaining" here because it's also
	// part of "fileDur". In practice, the final delay won't actually
	// be waited for.
	remaining := max(deadline.Sub(time.Now()).Seconds(), 0.0) + delay.Seconds()
	fileDur := file.Duration + delay.Seconds()
	return int(math.Floor(remaining / fileDur))
}

// ---------------------------------------------------------------------
// File holds the information needed to access one MP3 file on a client.

type File struct {
	// Location of the file on the device.
	Folder, File	int

	// The duration of the file, in seconds.
	// Should not include any delay imposed by the behavior of the client.
	Duration	float64
}

func (f File) String() string {
	return fmt.Sprintf("%d/%02d (%.3f)", f.Folder, f.File, f.Duration)
}

// ---------------------------------------------------------------------
// Play holds the information for a single file-play command.

type Play struct {
	File	File
	Reps	int
	Delay	time.Duration
	Jitter	time.Duration
}

// The expected duration of this command.
// Doesn't take jitter into account, because that happens on the client.
func (p *Play) Duration() time.Duration {
	delay := p.Delay.Seconds()
	d := (p.File.Duration + delay) * float64(p.Reps)
	if (p.Reps > 0) {
		d -= delay	// don't delay after the last one
	}
	return time.Duration(d * float64(time.Second))
}

func (p Play) String() string {
	return fmt.Sprintf("%2d/%2d (%d reps, %.3f delay, %.3f jitter, expected time %.2f sec)",
            p.File.Folder, p.File.File, p.Reps,
	    p.Delay.Seconds(), p.Jitter.Seconds(), p.Duration().Seconds())
}

