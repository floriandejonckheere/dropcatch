.PHONY: build web test check fakeregistry screenshots

build: web
	CGO_ENABLED=0 go build -trimpath -o dropcatch ./cmd/dropcatch

web:
	cd web && npm ci && npm run build

test:
	go vet ./...
	go test ./...

check: test
	cd web && npm run check

# Runs a fake RDAP registry for local development (see scripts/fakeregistry).
fakeregistry:
	go run ./scripts/fakeregistry

# Regenerates docs/screenshots (requires Playwright's Chromium).
screenshots:
	./scripts/screenshots.sh
