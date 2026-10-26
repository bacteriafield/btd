package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/copr"
	"github.com/bacteriafield/btd/pkg/dnf"
	"github.com/bacteriafield/btd/pkg/menu"
	"github.com/bacteriafield/btd/pkg/settings"
)

// target is a spec for dnf install plus the Copr project, if any, to
// enable before.
type target struct {
	name string
	copr string // owner/project, empty for the enabled repositories
}

func install(cfg *settings.Configuration, args []string) int {
	var targets []target
	var flags []string
	for _, arg := range args {
		// ponytail: dnf options need the --opt=value form; a separate value is taken as a target
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			continue
		}
		t, err := resolve(cfg, arg)
		if err != nil {
			return fail(err)
		}
		targets = append(targets, t)
	}
	return installTargets(cfg, targets, flags)
}

// resolve finds where arg comes from: an explicit owner/project/pkg, a path
// or URL left to dnf, the enabled repositories, or a Copr project the user
// picks.
func resolve(cfg *settings.Configuration, arg string) (target, error) {
	if t, ok := explicitCopr(arg); ok {
		return t, confirmCopr(t.copr)
	}
	if strings.Contains(arg, "/") || strings.HasSuffix(arg, ".rpm") {
		return target{name: arg}, nil
	}

	provided, err := dnf.Provided(cfg, arg)
	if err != nil || provided {
		return target{name: arg}, err
	}

	// -y never picks a third-party repository on its own.
	if settings.NoConfirm {
		return target{}, errors.New(gotext.Get("%[1]s is not in the enabled repositories; use owner/project/%[1]s to install it from Copr with -y", arg))
	}

	fmt.Fprintln(os.Stderr, gotext.Get("%s is not in the enabled repositories, searching Copr...", arg))
	projects, err := copr.Search(cfg, arg)
	if err != nil {
		return target{}, err
	}
	if len(projects) == 0 {
		return target{}, errors.New(gotext.Get("target not found: %s", arg))
	}

	res := make([]result, len(projects))
	for i, p := range projects {
		res[i] = result{Item: menu.Item{Source: "copr:" + p.FullName, Description: firstLine(p.Description)}, copr: &p}
	}
	rank(res, arg)
	printResults(cfg, res)

	i, err := pickOne(gotext.Get("Copr project to use: "), len(res))
	if err != nil {
		return target{}, err
	}
	return fromCopr(cfg, *res[i].copr, arg)
}

// explicitCopr parses owner/project/pkg; paths, URLs and local rpms are not one.
func explicitCopr(arg string) (target, bool) {
	parts := strings.Split(arg, "/")
	if len(parts) != 3 || slices.Contains(parts, "") || strings.HasPrefix(arg, ".") ||
		strings.Contains(arg, ":") || strings.HasSuffix(arg, ".rpm") {
		return target{}, false
	}
	return target{name: parts[2], copr: parts[0] + "/" + parts[1]}, true
}

// fromCopr picks the package named term in a Copr project, or lets the user
// choose one, then asks before enabling the project.
func fromCopr(cfg *settings.Configuration, p copr.Project, term string) (target, error) {
	pkgs, err := copr.Packages(cfg, p)
	if err != nil {
		return target{}, err
	}

	name := term
	if !slices.Contains(pkgs, term) {
		if settings.NoConfirm || len(pkgs) == 0 {
			return target{}, errors.New(gotext.Get("no package named %s in %s", term, p.FullName))
		}
		res := make([]result, len(pkgs))
		for i, pkg := range pkgs {
			res[i] = result{Item: menu.Item{Source: "copr:" + p.FullName, Name: pkg}}
		}
		printResults(cfg, res)

		i, err := pickOne(gotext.Get("Package to install: "), len(pkgs))
		if err != nil {
			return target{}, err
		}
		name = pkgs[i]
	}
	return target{name: name, copr: p.FullName}, confirmCopr(p.FullName)
}

// confirmCopr asks before enabling a Copr project, which can replace any
// package on the system once enabled.
func confirmCopr(project string) error {
	warn(gotext.Get("%s is a third-party Copr repository: once enabled, it can replace any package on your system.", project))
	if !confirm(gotext.Get("Enable %s?", project)) {
		return errors.New(gotext.Get("aborted"))
	}
	return nil
}

// installTargets enables the Copr projects the targets need, then installs
// all of them in a single dnf transaction.
func installTargets(cfg *settings.Configuration, targets []target, flags []string) int {
	args := append([]string{"install"}, flags...)
	enabled := map[string]bool{}
	for _, t := range targets {
		if t.copr != "" && !enabled[t.copr] {
			enabled[t.copr] = true
			// btd already asked, so dnf's own Copr disclaimer is answered with -y.
			if err := dnf.Run(cfg, true, "copr", "enable", t.copr, "-y"); err != nil {
				return exitCode(err)
			}
		}
		args = append(args, t.name)
	}
	return exitCode(dnf.Run(cfg, true, args...))
}
