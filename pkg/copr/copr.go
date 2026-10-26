// Package copr talks to the Copr API v3.
package copr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/settings"
)

// Project is a Copr project; Name is the project name without the owner.
type Project struct {
	Owner       string            `json:"ownername"`
	Name        string            `json:"name"`
	FullName    string            `json:"full_name"`
	Description string            `json:"description"`
	ChrootRepos map[string]string `json:"chroot_repos"`
}

// Copr's project search takes 10-17s server side, limit or not (Oct 2026).
var client = &http.Client{Timeout: 30 * time.Second}

// Search returns the projects matching query that have builds for this
// system's chroot, in the API's order.
func Search(cfg *settings.Configuration, query string) ([]Project, error) {
	chroot, err := chroot()
	if err != nil {
		return nil, err
	}

	var page struct {
		Items []Project `json:"items"`
	}
	if err := get(cfg, "/api_3/project/search?"+url.Values{"query": {query}}.Encode(), &page); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(page.Items, func(p Project) bool {
		_, built := p.ChrootRepos[chroot]
		return !built
	}), nil
}

func get(cfg *settings.Configuration, path string, v any) error {
	u := strings.TrimSuffix(cfg.CoprURL, "/") + path
	cfg.Debugf("GET %s", u)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "btd/"+cfg.Version)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("copr: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// chroot returns this system's Copr chroot, e.g. fedora-44-x86_64; rawhide
// sets REDHAT_SUPPORT_PRODUCT_VERSION=rawhide, giving fedora-rawhide-x86_64.
func chroot() (string, error) {
	osRelease, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", err
	}

	var version string
	for line := range strings.Lines(string(osRelease)) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "REDHAT_SUPPORT_PRODUCT_VERSION="); ok {
			version = strings.Trim(v, `"'`)
		}
	}
	if version == "" {
		return "", errors.New(gotext.Get("unsupported system: no REDHAT_SUPPORT_PRODUCT_VERSION in /etc/os-release"))
	}

	arch, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "", err
	}
	return "fedora-" + version + "-" + strings.TrimSpace(string(arch)), nil
}
