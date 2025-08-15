package log

import "fmt"
import "log"

type Level int
const (
	Fatal Level = iota
	Error
	Warning
	Info
	Debug
)

var DebugLevel = Debug

func Logf(l Level, format string, v ...any) {
	switch l {
	case Fatal:
		Fatalf(format, v...)
	case Error:
		Errorf(format, v...)
	case Warning:
		Warningf(format, v...)
	case Info:
		Infof(format, v...)
	case Debug:
		Debugf(format, v...)
	}
}

func Debugf(format string, v ...any) {
	if (DebugLevel < Debug) {
		return
	}
	log.Printf(fmt.Sprintf("[D] %s", format), v...)
}

func Infof(format string, v ...any) {
	if (DebugLevel < Info) {
		return
	}
	log.Printf(fmt.Sprintf("[I] %s", format), v...)
}

func Warningf(format string, v ...any) {
	if (DebugLevel < Warning) {
		return
	}
	log.Printf(fmt.Sprintf("[W] %s", format), v...)
}

func Errorf(format string, v ...any) {
	if (DebugLevel < Error) {
		return
	}
	log.Printf(fmt.Sprintf("[E] %s", format), v...)
}

func Fatalf(format string, v ...any) {
	log.Fatalf(fmt.Sprintf("[F] %s", format), v...)
}

func Panicf(format string, v ...any) {
	log.Panicf(fmt.Sprintf("[F] %s", format), v...)
}
