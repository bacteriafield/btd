package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/settings"
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
	gotext.Configure(locale, language(), "btd")
}

// language picks the UI language with gettext's precedence:
// LANGUAGE, LC_ALL, LC_MESSAGES, then LANG.
func language() string {
	for _, env := range [...]string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		lang, _, _ := strings.Cut(os.Getenv(env), ":")
		lang, _, _ = strings.Cut(lang, ".") // pt_BR.UTF-8 -> pt_BR
		lang, _, _ = strings.Cut(lang, "@")
		if lang != "" && lang != "C" && lang != "POSIX" {
			return lang
		}
	}
	return "en"
}

func main() {
	os.Exit(run())
}

func run() (ret int) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintln(os.Stderr, gotext.Get("panic: %v", rec))
			fmt.Fprintln(os.Stderr, string(debug.Stack()))
			ret = 1
		}
	}()

	if os.Geteuid() == 0 {
		warn(gotext.Get("avoid running btd as root"))
	}

	configPath, err := settings.ConfigPath()
	if err != nil {
		return fail(err)
	}
	cfg := settings.NewConfig(configPath, btdVersion)

	rest, err := parseFlags(os.Args[1:], cfg)
	if err != nil {
		return fail(err)
	}

	if cfg.SaveConfig {
		if err := cfg.Save(configPath); err != nil {
			return fail(err)
		}
		if len(rest) == 0 {
			return 0
		}
	}

	return handleCmd(cfg, rest)
}
