package main

import (
	"slices"
	"testing"

	"github.com/bacteriafield/btd/pkg/copr"
	"github.com/bacteriafield/btd/pkg/menu"
)

func TestRank(t *testing.T) {
	pkg := func(name string) result { return result{Item: menu.Item{Name: name}} }
	project := func(name string) result { return result{copr: &copr.Project{Name: name}} }

	res := []result{project("zed-nightly"), pkg("python-zed"), project("tools"), pkg("Zed"), project("zed")}
	rank(res, "zed")

	var got []string
	for _, r := range res {
		if r.copr != nil {
			got = append(got, "copr:"+r.copr.Name)
		} else {
			got = append(got, r.Name)
		}
	}
	if want := []string{"Zed", "copr:zed", "copr:zed-nightly", "python-zed", "copr:tools"}; !slices.Equal(got, want) {
		t.Errorf("rank() = %q, want %q", got, want)
	}
}
