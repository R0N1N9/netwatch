#!/bin/sh
# Update NetWatch and start it again:
#   stop the running copy, pull the latest code, rebuild, give the new build
#   its capture rights, start the console.
# Anything after the script name is passed to "netwatch gui":   ./update.sh --browser
# To update without starting:                                   ./update.sh --no-start
set -e
cd "$(dirname "$0")"
say() { printf '\n\033[1;36m▚ %s\033[0m\n' "$1"; }

start=1
if [ "$1" = "--no-start" ]; then start=0; shift; fi

say "stopping NetWatch if it is running"
pkill -x netwatch 2>/dev/null || true
if pgrep -x netwatch >/dev/null 2>&1; then # a copy started with sudo
	sudo pkill -x netwatch || true
fi
i=0
while pgrep -x netwatch >/dev/null 2>&1 && [ "$i" -lt 25 ]; do sleep 0.2; i=$((i + 1)); done

say "getting the latest code"
git pull --ff-only

say "building"
make build

say "giving the new build its capture rights (this may ask for your password)"
sudo setcap cap_net_raw,cap_net_admin+eip ./bin/netwatch || echo "  could not set them — live capture will need sudo; --demo still works"

if [ "$start" = 1 ]; then
	say "starting NetWatch"
	exec ./bin/netwatch gui "$@"
fi
say "done — start it with: ./bin/netwatch gui"
