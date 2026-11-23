PREFIX    ?= /usr/local
LOCALEDIR ?= $(PREFIX)/share/locale
DESTDIR   ?=
VERSION   ?= 
IMAGE     ?= btd
ARGS      ?= btd -s neovim
BENCHFLAGS ?=

# go finds its own installation. A GOROOT exported by a version manager
# (gvm) can point at another Go: when the go running is newer (go.mod's
# toolchain line, or an editor putting a toolchain first in PATH), it then
# runs that older Go's compiler and fails with "compile: version ... does
# not match go tool version".
GO      := env -u GOROOT go
LDFLAGS := -X main.locale=$(LOCALEDIR) $(if $(VERSION),-X main.btdVersion=$(VERSION))
LANGS   := $(basename $(notdir $(wildcard po/*.po)))
GOBUILD := $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o btd .

.PHONY: build test bench install uninstall pot docker docker-run docker-bench docker-test clean

build:
	$(GOBUILD)

btd:
	$(GOBUILD)

test:
	$(GO) test ./...

## btd against dnf5 on this Fedora machine; BENCHFLAGS=-short skips Copr.
bench:
	$(GO) test -run '^$$' -bench . -benchtime 5x $(BENCHFLAGS) ./benchmark

po/%.mo: po/%.po
	msgfmt -c -o $@ $<

install: btd $(LANGS:%=po/%.mo)
	install -Dm755 btd $(DESTDIR)$(PREFIX)/bin/btd
	for lang in $(LANGS); do \
		install -Dm644 po/$$lang.mo $(DESTDIR)$(LOCALEDIR)/$$lang/LC_MESSAGES/btd.mo; \
	done

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/btd $(LANGS:%=$(DESTDIR)$(LOCALEDIR)/%/LC_MESSAGES/btd.mo)

## Regenerates po/btd.pot from the gotext.Get calls.
pot:
	$(GO) run github.com/leonelquinteros/gotext/cli/xgotext@v1.7.2 -pkg-tree . -out po -default btd

## Fedora image with btd and its translations, for testing.
docker:
	docker build -t $(IMAGE) .

docker-run: docker
	docker run --rm -it $(IMAGE) $(ARGS)

## make bench in a Fedora container, as root, so the install benchmark runs too.
docker-bench:
	docker run --rm -v "$(CURDIR)":/src:ro -w /src fedora:latest \
		bash -c 'dnf -yq install golang make && make bench BENCHFLAGS="$(BENCHFLAGS)"'

## go test in a Fedora container, as root, so the tests that compare btd
## with dnf5 run too (elsewhere they skip).
docker-test:
	docker run --rm -v "$(CURDIR)":/src:ro -w /src -e GOFLAGS=-buildvcs=false fedora:latest \
		bash -c 'dnf -yq install golang && dnf5 -q makecache && go test ./...'

clean:
	rm -f btd po/*.mo
