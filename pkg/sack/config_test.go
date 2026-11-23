package sack

import "testing"

func TestCacheDirName(t *testing.T) {
	c := &config{vars: map[string]string{"releasever": "45", "basearch": "x86_64"}}
	r := &repoConfig{id: "fedora", opts: options{
		"metalink": "https://mirrors.fedoraproject.org/metalink?repo=fedora-$releasever&arch=${basearch}",
	}}
	// The directory dnf5 5.4 created for this repository on Fedora 45.
	if got, err := c.cacheDirName(r); err != nil || got != "fedora-cacf678974f18610" {
		t.Errorf("cacheDirName = %q, %v", got, err)
	}
	if _, err := c.substitute("${releasever:-44}"); err == nil {
		t.Error("${var:-word}: want ErrUnsupported")
	}
	if got, _ := c.substitute("$unknown/$basearch"); got != "$unknown/x86_64" {
		t.Errorf("substitute = %q", got)
	}
}

func TestParseSeconds(t *testing.T) {
	for in, want := range map[string]int64{"6h": 21600, "7d": 604800, "90": 90, "1.5m": 90, "never": -1, "-1": -1} {
		if got, err := parseSeconds(in); err != nil || got != want {
			t.Errorf("parseSeconds(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}
