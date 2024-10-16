package fileset

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"time"
)

// Config describes a set of files that are operated on together.
type Config struct {
	Regex		string	// matches key in file map
}

// File holds the information needed to access one MP3 file on a client.
type File struct {
	// Location of the file on the device.
	Folder, File	int

	// The duration of the file, in seconds.
	// Should not include any delay imposed by the behavior of the client.
	Duration	float64
}

func (f *File) SleepForDuration() {
	time.Sleep(time.Duration(f.Duration * float64(time.Second)))
}

// ---------------------------------------------------------------------

// Set is the runtime instantiation of a file set.
type Set struct {
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
	return &Set{
		files:	results,
	}, nil
}

func (f *Set) Pick() File {
	return f.files[rand.Int32N(int32(len(f.files)))]
}

func (f *Set) Set() []File {
	return f.files
}
