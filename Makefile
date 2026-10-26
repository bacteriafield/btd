PREFIX    ?= /usr/local
LOCALEDIR ?= $(PREFIX)/share/locale
DESTDIR   ?=
VERSION   ?= 
IMAGE     ?= btd
ARGS      ?= btd -s neovim

LDFLAGS := -X main.locale=$(LOCALEDIR) $(if $(VERSION),-X main.btdVersion=$(VERSION))
LANGS   := $(basename $(notdir $(wildcard po/*.po)))
GOBUILD := go build -trimpath -ldflags "$(LDFLAGS)" -o btd .

.PHONY: build test install uninstall pot docker docker-run clean

build:
	$(GOBUILD)

btd:
	$(GOBUILD)

test:
	go test ./...

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
	go run github.com/leonelquinteros/gotext/cli/xgotext@v1.7.2 -pkg-tree . -out po -default btd

## Fedora image with btd and its translations, for testing.
docker:
	docker build -t $(IMAGE) .

docker-run: docker
	docker run --rm -it $(IMAGE) $(ARGS)

clean:
	rm -f btd po/*.mo
