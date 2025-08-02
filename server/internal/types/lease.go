package types

import (
	"fmt"
	"strings"
)

type LeaseType int
const (
	Sound LeaseType = iota
	Light
)

func ValidLeaseTypes() []LeaseType {
	return []LeaseType{Sound, Light}
}

func (lt *LeaseType) unmarshalString(s string) error {
	switch strings.ToLower(s) {
	case "sound":
		*lt = Sound
		return nil
	case "light":
		*lt = Light
		return nil
	default:
		return fmt.Errorf("unknown lease type %q", s)
	}
}

func (lt LeaseType) String() string {
	switch (lt) {
	case Sound:
		return "sound"
	case Light:
		return "light"
	default:
		return "unknown type"
	}
}

func (lt *LeaseType) UnmarshalText(b []byte) error {
	return lt.unmarshalString(string(b))
}
