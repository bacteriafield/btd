package main

import (
	"context"
	"os"
	"os/signal"
	"strings"

	"github.com/leonelquinteros/gotext"
)

var (
	// to be set on compile
	btdVersion = "0.0.1"
	// to be set on compile
	locale = "/usr/share/locale"
)

func init() {
	if envLocale := os.Getenv("LOCALE"); envLocale != "" {
		locale = envLocale
	}

	if lc := os.Getenv("LANGUAGE"); lc != "" {
		locales := strings.Split(lc, ":")
		if len(locales) > 0 && locales[0] != "" {
			gotext.Configure(locale, locales[0], "btd")
		}
	} else {
		gotext.Configure(locale, "en", "btd")
	}
}

func newSignalContext() (context.Context, context.CancelFunc) { return signal.NotifyContext(context.Background(), os.Interrupt) }

func main() {
	fallbackLog := log.New(os.Stderr, "btd -> FallBack:  ", log.Lshortfile)
	var (
		err error
		ctx, stop = newSignalContext()
		ret = 0
	)
	defer stop()

	defer func() {
		if rec := recover(); rec != nil {
			fallbackLog.Errorf("Panic occured: %v", rec)
			fallbackLog.Errorf("Stacktrace: %s", string(debug.Stack()))
			ret = 1
		}
		os.Exit()
	}()

	init()

	if os.Geteuid() == 0 {
		fallbackLog.Warn("Running as root")
	}

	//configPath := settings
}
