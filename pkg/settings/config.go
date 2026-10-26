package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bacteriafield/btd/pkg"
	"github.com/leonelquinteros/gotext"
)

// NoConfirm skips btd's yes/no questions and passes -y to dnf.
var NoConfirm = false

type Configuration struct {
	CoprURL           string `json:"copr_url"`
	DnfBin            string `json:"dnf_bin"`
	SudoBin           string `json:"sudo_bin"`
	SudoFlags         string `json:"sudo_flags"`
	Version           string `json:"version"`
	BottomUp          bool   `json:"bottom_up"`
	SingleLineResults bool   `json:"single_line_results"`
	Debug             bool   `json:"debug"`
	SaveConfig        bool   `json:"-"`
}

func DefaultConfig(version string) *Configuration {
	return &Configuration{
		CoprURL:  "https://copr.fedorainfracloud.org",
		DnfBin:   "dnf5",
		SudoBin:  "sudo",
		Version:  version,
		BottomUp: true,
	}
}

// ConfigPath returns $XDG_CONFIG_HOME/btd/config.json, falling back to ~/.config.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "btd", "config.json"), nil
}

// NewConfig returns the defaults overridden by the config file and BTD_SUDO.
func NewConfig(configPath, version string) *Configuration {
	c := DefaultConfig(version)
	c.load(configPath)
	c.Version = version

	if sudo := os.Getenv("BTD_SUDO"); sudo != "" {
		if sudo != c.SudoBin {
			c.SudoFlags = "" // the configured flags were meant for another tool
		}
		c.SudoBin = sudo
	}

	c.expandEnv()
	return c
}

// Save writes btd config to file.
func (c *Configuration) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Debugf prints a debug line to stderr when --debug is on.
func (c *Configuration) Debugf(format string, a ...any) {
	if c.Debug {
		fmt.Fprintf(os.Stderr, "debug: "+format+"\n", a...)
	}
}

// SetPrivilege picks the tool that runs dnf as root: the configured one,
// then sudo, run0, doas and pkexec.
func (c *Configuration) SetPrivilege() error {
	for _, bin := range [...]string{c.SudoBin, "sudo"} {
		if _, err := exec.LookPath(bin); err == nil {
			c.SudoBin = bin
			return nil
		}
	}

	c.SudoFlags = "" // the flags were meant for the configured tool
	for _, bin := range [...]string{"run0", "doas", "pkexec"} {
		if _, err := exec.LookPath(bin); err == nil {
			c.SudoBin = bin
			return nil
		}
	}

	return &pkg.ErrPrivilegeElevatorNotFound{confValue: c.SudoBin}
}

func (c *Configuration) load(path string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(data, c)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, gotext.Get("failed to read config file '%s': %s", path, err))
	}
}

func (c *Configuration) expandEnv() {
	c.CoprURL = os.ExpandEnv(c.CoprURL)
	c.DnfBin = expandEnvOrHome(c.DnfBin)
	c.SudoBin = expandEnvOrHome(c.SudoBin)
	c.SudoFlags = os.ExpandEnv(c.SudoFlags)
}

func expandEnvOrHome(path string) string {
	path = os.ExpandEnv(path)
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		path = filepath.Join(os.Getenv("HOME"), rest)
	}
	return path
}
