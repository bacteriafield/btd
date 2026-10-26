package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/menu"
	"github.com/bacteriafield/btd/pkg/settings"
)

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

// pickOne asks for exactly one number of an n-item menu.
func pickOne(question string, n int) (int, error) {
	picked, err := menu.ParseSelection(ask(question), n)
	if err != nil {
		return 0, err
	}
	if len(picked) != 1 {
		return 0, errors.New(gotext.Get("pick exactly one number"))
	}
	return picked[0], nil
}

// confirm asks a yes/no question that defaults to no; -y answers yes.
func confirm(question string) bool {
	if settings.NoConfirm {
		return true
	}
	answer := strings.ToLower(ask(question + " [y/N] "))
	return answer == "y" || answer == "yes"
}
