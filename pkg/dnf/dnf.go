// Package dnf runs dnf5: queries as the user, transactions as root.
package dnf

import (
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"

	"github.com/bacteriafield/btd/pkg/settings"
)

// Run runs dnf attached to the terminal, through the privilege elevator
// when root is set and btd is not root already.
func Run(cfg *settings.Configuration, root bool, args ...string) error {
	if settings.NoConfirm {
		args = append(slices.Clip(args), "-y")
	}

	name, argv := cfg.DnfBin, args
	if root && os.Geteuid() != 0 {
		if err := cfg.SetPrivilege(); err != nil {
			return err
		}
		name = cfg.SudoBin
		argv = append(append(strings.Fields(cfg.SudoFlags), cfg.DnfBin), args...)
	}
	cfg.Debugf("%s %s", name, strings.Join(argv, " "))

	// No exec.CommandContext: killing dnf mid-transaction can break the rpmdb.
	// dnf gets the terminal's Ctrl+C itself; btd just waits for it to exit.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	cmd := exec.Command(name, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
