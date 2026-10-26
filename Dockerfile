# Fedora test box with btd and its translations: make docker-run
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
# PREFIX=/usr points btd at /usr/share/locale, where the .po files go below.
RUN CGO_ENABLED=0 make build PREFIX=/usr

FROM fedora:latest
COPY --from=build /src/btd /usr/local/bin/btd
COPY po /tmp/po
# gotext reads .po files directly, so no msgfmt needed here.
RUN for po in /tmp/po/*.po; do \
        dir=/usr/share/locale/$(basename "$po" .po)/LC_MESSAGES; \
        mkdir -p "$dir" && cp "$po" "$dir/btd.po"; \
    done && rm -r /tmp/po
CMD ["bash"]
