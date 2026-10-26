package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/dnf"
	"github.com/bacteriafield/btd/pkg/settings"
)

// btdCommands maps btd's own commands and their aliases, dnf's aliases
// included, so `btd in foo` still gets the Copr fallback.
var btdCommands = map[string]string{
	"install": "install", "in": "install", "-i": "install",
	"search": "search", "se": "search", "-s": "search",
	"upgrade": "upgrade", "up": "upgrade", "update": "upgrade", "-u": "upgrade",
	"remove": "remove", "rm": "remove", "-r": "remove",
	"-h": "help", "--help": "help",
	"--version": "version",
}

// ponytail: dnf5 5.4 command list; a command added to dnf5 must be added
// here too, otherwise `btd <command>` searches for it instead.
var dnfCommands = map[string]bool{
	"do": true, "distro-sync": true, "downgrade": true, "reinstall": true,
	"debuginfo-install": true, "swap": true, "mark": true, "autoremove": true,
	"provides": true, "replay": true, "check-upgrade": true, "check": true,
	"leaves": true, "repoquery": true, "list": true, "info": true,
	"group": true, "environment": true, "module": true, "history": true,
	"repo": true, "advisory": true, "versionlock": true, "system-upgrade": true,
	"offline-distrosync": true, "offline-upgrade": true, "offline": true,
	"config-manager": true, "check-update": true, "dg": true, "dsync": true,
	"grp": true, "if": true, "ls": true, "mc": true, "rei": true,
	"repoinfo": true, "repolist": true, "rq": true, "updateinfo": true,
	"upgrade-minimal": true, "clean": true, "download": true, "makecache": true,
	"builddep": true, "build-dep": true, "changelog": true, "copr": true,
	"needs-restarting": true, "repoclosure": true, "repomanage": true,
	"reposync": true,
}

// readOnly are the dnf commands that run as the user; the rest need root.
var readOnly = map[string]bool{
	"list": true, "ls": true, "info": true, "if": true, "repoquery": true,
	"rq": true, "provides": true, "check-upgrade": true, "check-update": true,
	"check": true, "leaves": true, "changelog": true, "download": true,
	"repoclosure": true, "needs-restarting": true, "repoinfo": true,
	"repolist": true,
}

// handleCmd runs the command in args: btd's own, a dnf passthrough, or the
// interactive search for anything else.
func handleCmd(cfg *settings.Configuration, args []string) int {
	if len(args) == 0 {
		return exitCode(dnf.Run(cfg, true, "upgrade"))
	}

	cmd, rest := args[0], args[1:]
	switch btdCommands[cmd] {
	case "install":
		return install(cfg, rest)
	case "search":
		return search(cfg, rest)
	case "upgrade", "remove":
		return exitCode(dnf.Run(cfg, true, append([]string{btdCommands[cmd]}, rest...)...))
	case "help":
		fmt.Print(usage())
		return 0
	case "version":
		fmt.Println("btd v" + btdVersion)
		return 0
	}

	if dnfCommands[cmd] || strings.HasPrefix(cmd, "-") {
		return exitCode(dnf.Run(cfg, needsRoot(args), args...))
	}

	return interactive(cfg, args)
}

// parseFlags applies btd's flags to cfg and returns the arguments left for
// the command; `--` stops parsing.
func parseFlags(args []string, cfg *settings.Configuration) ([]string, error) {
	strFlags := map[string]*string{
		"--copr-url":   &cfg.CoprURL,
		"--dnf-bin":    &cfg.DnfBin,
		"--sudo-bin":   &cfg.SudoBin,
		"--sudo-flags": &cfg.SudoFlags,
	}
	boolFlags := map[string]*bool{
		"--bottom-up":           &cfg.BottomUp,
		"--single-line-results": &cfg.SingleLineResults,
		"--debug":               &cfg.Debug,
	}

	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")

		if p, ok := strFlags[name]; ok {
			if !hasValue {
				if i+1 == len(args) {
					return nil, errors.New(gotext.Get("flag %s requires a value", name))
				}
				i++
				value = args[i]
			}
			*p = value
			continue
		}
		if p, ok := boolFlags[arg]; ok {
			*p = true
			continue
		}
		if negated, ok := strings.CutPrefix(arg, "--no-"); ok && boolFlags["--"+negated] != nil {
			*boolFlags["--"+negated] = false
			continue
		}

		switch arg {
		case "--save":
			cfg.SaveConfig = true
		case "-y", "--assumeyes":
			settings.NoConfirm = true
		case "--":
			return append(rest, args[i+1:]...), nil
		default:
			rest = append(rest, arg)
		}
	}
	return rest, nil
}

// needsRoot reports whether a passed-through dnf call must be elevated:
// read-only commands and `<command> list|info` run as the user.
func needsRoot(args []string) bool {
	var words []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			words = append(words, arg)
		}
	}
	switch {
	case len(words) == 0:
		return true
	case readOnly[words[0]]:
		return false
	case len(words) > 1 && (words[1] == "list" || words[1] == "info"):
		return false
	}
	return true
}

// exitCode turns a dnf error into btd's exit code, keeping dnf's own code.
func exitCode(err error) int {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
