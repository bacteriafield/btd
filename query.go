package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/copr"
	"github.com/bacteriafield/btd/pkg/dnf"
	"github.com/bacteriafield/btd/pkg/menu"
	"github.com/bacteriafield/btd/pkg/settings"
)

// result is a search hit: a package from the enabled repositories, or a
// Copr project when copr is set.
type result struct {
	menu.Item
	copr *copr.Project
}

// results searches the enabled repositories and Copr; a Copr failure only
// leaves its projects out.
func results(cfg *settings.Configuration, terms []string) ([]result, error) {
	if len(terms) == 0 {
		return nil, errors.New(gotext.Get("no search terms given"))
	}
	query := strings.Join(terms, " ")
	fmt.Fprintln(os.Stderr, gotext.Get("Searching the repositories and Copr..."))

	// Copr is slow, so it runs while dnf searches.
	var projects []copr.Project
	var coprErr error
	var wg sync.WaitGroup
	wg.Go(func() { projects, coprErr = copr.Search(cfg, query) })
	pkgs, err := dnf.Search(cfg, terms)
	wg.Wait()

	if err != nil {
		return nil, err
	}
	var res []result
	for _, p := range pkgs {
		res = append(res, result{Item: menu.Item{Source: p.Repo, Name: p.Name, Version: p.EVR, Description: p.Summary}})
	}

	if coprErr != nil {
		warn(gotext.Get("Copr search failed, showing the enabled repositories only: %s", coprErr))
	}
	for _, p := range projects {
		res = append(res, result{Item: menu.Item{Source: "copr:" + p.FullName, Description: firstLine(p.Description)}, copr: &p})
	}

	rank(res, query)
	return res, nil
}

// rank orders results by exact name, then name containing query, then the
// rest; ties keep the enabled repositories before Copr.
func rank(res []result, query string) {
	slices.SortStableFunc(res, func(a, b result) int { return score(a, query) - score(b, query) })
}

// naive name-match ranking; switch to a scored match if results feel off.
func score(r result, query string) int {
	name := r.Name
	if r.copr != nil {
		name = r.copr.Name
	}
	name, query = strings.ToLower(name), strings.ToLower(query)
	switch {
	case name == query:
		return 0
	case strings.Contains(name, query):
		return 1
	}
	return 2
}

// firstLine trims a Copr description, often paragraphs of markdown, to its
// first line.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
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

	var targets []target
	for _, i := range picked {
		if res[i].copr == nil {
			targets = append(targets, target{name: res[i].Name})
			continue
		}
		t, err := fromCopr(cfg, *res[i].copr, strings.Join(terms, " "))
		if err != nil {
			return fail(err)
		}
		targets = append(targets, t)
	}
	return installTargets(cfg, targets, nil)
}
