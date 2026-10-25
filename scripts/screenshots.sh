#!/bin/sh
# Regenerates the README screenshots in docs/screenshots/.
#
# Builds Dropcatch, starts it with the fake registry (scripts/fakeregistry)
# and a stand-in for Apprise, and captures the screenshots with Playwright.
# Requires Go, Node and a Chromium for Playwright (`cd web && npx playwright
# install chromium`, or point CHROMIUM_PATH at an existing one).
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
pids=
cleanup() {
  for p in $pids; do kill "$p" 2>/dev/null || true; done
  rm -rf "$work"
}
trap cleanup EXIT INT TERM

(cd "$root/web" && npm run build --silent >/dev/null)
(cd "$root" && CGO_ENABLED=0 go build -o "$work/dropcatch" ./cmd/dropcatch)
(cd "$root" && CGO_ENABLED=0 go build -o "$work/fakeregistry" ./scripts/fakeregistry)

# Notifications are not sent anywhere.
printf '#!/bin/sh\nexit 0\n' >"$work/apprise"
chmod +x "$work/apprise"

# A VPS watch with a week of stock history.
(cd "$root/web" && node scripts/seed.mjs "$work/data")

port=${PORT:-8765}
registry=${REGISTRY_PORT:-8053}
"$work/fakeregistry" --listen "127.0.0.1:$registry" >"$work/registry.log" 2>&1 &
pids="$pids $!"
"$work/dropcatch" --data-dir "$work/data" --listen "127.0.0.1:$port" --apprise "$work/apprise" \
  --rdap-bootstrap "http://127.0.0.1:$registry/dns.json" --ovh-api "http://127.0.0.1:$registry/v1" >"$work/server.log" 2>&1 &
pids="$pids $!"
for _ in $(seq 50); do
  curl -sf "http://127.0.0.1:$port/api/config" >/dev/null && break
  sleep 0.2
done

(cd "$root/web" && DROPCATCH_URL="http://127.0.0.1:$port/" OUT="$root/docs/screenshots" node scripts/screenshots.mjs)
