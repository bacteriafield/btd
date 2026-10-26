package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/dnf"
	"github.com/bacteriafield/btd/pkg/menu"
	"github.com/bacteriafield/btd/pkg/settings"
)

var (
	// to be set on compile
	btdVersion = "0.0.1"
	// to be set on compile
	locale = "/usr/share/locale"
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

	if len(rest) == 0 {
		return exitCode(dnf.Run(cfg, true, "upgrade"))
	}

	cmd, args := rest[0], rest[1:]
	switch btdCommands[cmd] {
	case "search":
		return search(cfg, args)
	case "upgrade", "remove":
		return exitCode(dnf.Run(cfg, true, append([]string{btdCommands[cmd]}, args...)...))
	case "help":
		fmt.Print(usage())
		return 0
	case "version":
		fmt.Println("btd v" + btdVersion)
		return 0
	}

	if dnfCommands[cmd] || strings.HasPrefix(cmd, "-") {
		return exitCode(dnf.Run(cfg, needsRoot(rest), rest...))
	}

	return interactive(cfg, rest)
}

// result is a search hit from the enabled repositories.
type result struct {
	menu.Item
}

// results searches the enabled repositories, exact name matches first.
func results(cfg *settings.Configuration, terms []string) ([]result, error) {
	if len(terms) == 0 {
		return nil, errors.New(gotext.Get("no search terms given"))
	}
	query := strings.Join(terms, " ")

	pkgs, err := dnf.Search(cfg, terms)
	if err != nil {
		return nil, err
	}

	var res []result
	for _, p := range pkgs {
		res = append(res, result{Item: menu.Item{Source: p.Repo, Name: p.Name, Version: p.EVR, Description: p.Summary}})
	}
	slices.SortStableFunc(res, func(a, b result) int { return score(a, query) - score(b, query) })
	return res, nil
}

// ponytail: naive name-match ranking; switch to a scored match if results feel off.
func score(r result, query string) int {
	name, query := strings.ToLower(r.Name), strings.ToLower(query)
	switch {
	case name == query:
		return 0
	case strings.Contains(name, query):
		return 1
	}
	return 2
}

func search(cfg *settings.Configuration, terms []string) int {
	res, err := results(cfg, terms)
	if err != nil {
		return fail(err)
	}
	if len(res) == 0 {
		fmt.Fprintln(os.Stderr, gotext.Get("no packages found"))
		return 1
	}
	printResults(cfg, res)
	return 0
}

// interactive is `btd <term>`: search, then install what the user picks.
func interactive(cfg *settings.Configuration, terms []string) int {
	res, err := results(cfg, terms)
	if err != nil {
		return fail(err)
	}
	if len(res) == 0 {
		fmt.Fprintln(os.Stderr, gotext.Get("no packages found"))
		return 1
	}
	printResults(cfg, res)

	picked, err := menu.ParseSelection(ask(gotext.Get("Packages to install (e.g. 1 2 3, 1-3): ")), len(res))
	if err != nil {
		return fail(err)
	}
	if len(picked) == 0 {
		return 0
	}

	names := []string{"install"}
	for _, i := range picked {
		names = append(names, res[i].Name)
	}
	return exitCode(dnf.Run(cfg, true, names...))
}

func printResults(cfg *settings.Configuration, res []result) {
	items := make([]menu.Item, len(res))
	for i, r := range res {
		items[i] = r.Item
	}
	menu.Print(os.Stdout, items, cfg.BottomUp, cfg.SingleLineResults)
}

var stdin = bufio.NewReader(os.Stdin)

// ask prints question and returns the answer; EOF reads as an empty answer.
func ask(question string) string {
	fmt.Print(question)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
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

func usage() string {
	return gotext.Get(`usage: btd [command] [options] [targets]

commands:
  btd                        upgrade the system
  btd <term>                 search official repos and Copr, then pick what to install
  btd install|in|-i <pkg>    install from official repos, falling back to Copr
  btd install owner/project/pkg
                             install pkg from that Copr project
  btd search|se|-s <term>    search official repos and Copr
  btd upgrade|up|-u          upgrade packages
  btd remove|rm|-r <pkg>     remove packages
  btd <dnf command>          any other dnf5 command is passed through

options:
  -h, --help                 show this help
  --version                  show btd's version
  -y, --assumeyes            answer yes to btd and dnf questions
  --save                     save the given options to the config file
  --copr-url <url>           Copr server
  --dnf-bin <path>           dnf5 binary
  --sudo-bin <path>          privilege elevator (sudo, run0, doas, pkexec)
  --sudo-flags <flags>       flags for the privilege elevator
  --[no-]bottom-up           show the best match last, next to the prompt
  --[no-]single-line-results show each result on one line
  --[no-]debug               print the commands and URLs btd runs
`)
}

func warn(msg string) {
	fmt.Fprintln(os.Stderr, gotext.Get("warning:"), msg)
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, gotext.Get("error:"), err)
	return 1
}
