# syntax=docker/dockerfile:1

# Builds for whatever machine runs the build, so `docker compose build`
# on a Raspberry Pi produces an arm64 (or 32-bit arm) image natively.
# Release images for other platforms come from buildx + QEMU in
# .github/workflows/docker-publish.yml.

FROM golang:1.27-alpine AS builder

RUN apk add --no-cache build-base pkgconf libusb-dev

# Set by docker-publish.yml to the git tag being released (e.g. v1.2.0);
# left as "dev" for a local build with no VERSION given.
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/doxie-scanner .

# Not scratch: gousb links libusb-1.0 dynamically through cgo, so the
# image needs libc and the libusb shared library.
FROM alpine:3.24

RUN apk add --no-cache libusb ca-certificates && \
    adduser -D -u 1000 doxie

COPY --from=builder /out/doxie-scanner /usr/local/bin/doxie-scanner

ENV DOXIE_DATA_DIR=/data
ENV DOXIE_LISTEN_ADDR=:8080

VOLUME /data
EXPOSE 8080

# Runs as root by default: USB device nodes under /dev/bus/usb are
# root-only unless the host has udev/99-doxie-scanner.rules installed.
# With the rule in place this can be switched to `USER doxie`.
ENTRYPOINT ["/usr/local/bin/doxie-scanner"]
