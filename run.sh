#!/bin/bash
set -e

# Runs the locally-built image (see build.sh) with the required data
# volume and USB device passthrough wired in. Rebuild first if you've
# changed anything: ./build.sh && ./run.sh

IMAGE="${IMAGE:-doxie-scanner:local}"
DATA_DIR="${DATA_DIR:-./data}"
PORT="${PORT:-8080}"

mkdir -p "$DATA_DIR"

docker rm -f doxie-scanner >/dev/null 2>&1 || true

ENV_ARGS=()
[ -f .env ] && ENV_ARGS=(--env-file .env)

docker run -d \
	--name doxie-scanner \
	--restart unless-stopped \
	"${ENV_ARGS[@]}" \
	-p "${PORT}:8080" \
	-v "$(realpath "$DATA_DIR")":/data \
	-v /etc/localtime:/etc/localtime:ro \
	-v /dev/bus/usb:/dev/bus/usb \
	--device-cgroup-rule='c 189:* rmw' \
	"$IMAGE"

echo "doxie-scanner running at http://localhost:${PORT}"
echo "logs: docker logs -f doxie-scanner"
