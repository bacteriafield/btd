# Fedora test box with btd and its translations:
#   docker build -t btd . && docker run --rm -it btd
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /btd .

FROM fedora:latest
COPY --from=build /btd /usr/local/bin/btd
COPY po /tmp/po
# gotext reads .po files directly, so no msgfmt needed here.
RUN for po in /tmp/po/*.po; do \
        dir=/usr/share/locale/$(basename "$po" .po)/LC_MESSAGES; \
        mkdir -p "$dir" && cp "$po" "$dir/btd.po"; \
    done && rm -r /tmp/po
CMD ["bash"]
