package main

import "testing"

func TestExplicitCopr(t *testing.T) {
	for arg, want := range map[string]target{
		"owner/project/pkg":   {name: "pkg", copr: "owner/project"},
		"@group/project/pkg":  {name: "pkg", copr: "@group/project"},
		"/usr/bin/vim":        {},
		"./a/b":               {},
		"dir/sub/foo.rpm":     {},
		"https://x.org/a.rpm": {},
		"owner/project":       {},
		"vim":                 {},
	} {
		got, ok := explicitCopr(arg)
		if got != want || ok != (want != target{}) {
			t.Errorf("explicitCopr(%q) = %+v, %v; want %+v", arg, got, ok, want)
		}
	}
}
