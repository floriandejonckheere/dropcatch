# Working on Dropcatch

Dropcatch is a self-hosted availability monitor for domain names and OVHcloud VPS: a Go server (single binary) that embeds a Svelte 5 web UI, checks domains over RDAP and WHOIS and VPS stock over OVH's public API, and sends notifications through the Apprise CLI. Read `README.md` for features and configuration and `docs/DESIGN.md` for architecture and decisions. The structure follows [Wavr](https://github.com/floriandejonckheere/wavr).

## Layout

```
cmd/dropcatch/        entry point
internal/config       flags and DROPCATCH_* environment variables
internal/auth         password login, signed session cookies
internal/store        state (settings, domains, VPS watches, targets) in dropcatch.json
internal/lookup       RDAP and WHOIS clients, domain name normalisation
internal/ovh          OVH VPS stock and plan catalog
internal/notify       Apprise CLI wrapper
internal/monitor      scheduler, status changes, notifications
internal/api          HTTP handlers and server-sent events
web/                  Svelte app (src/), built into web/dist and embedded
scripts/              fake registry (RDAP and OVH API) and screenshot generator
docker/               entrypoint, healthcheck, Apprise requirements
docs/                 design doc and README screenshots
```

## Commands

```sh
make check         # go vet, Go tests and the frontend type check; run before every commit
make build         # web UI + ./dropcatch binary
make fakeregistry  # fake RDAP registry and OVH API on port 8053
make screenshots   # regenerate docs/screenshots (needs Playwright Chromium)
gofmt -l .         # must print nothing; CI fails otherwise
```

The frontend must be built (`cd web && npm run build`) before `go build` embeds a current UI; `go test` works without it.

Run the app locally without network access: `make fakeregistry` in one terminal, then `make build && ./dropcatch --data-dir /tmp/dropcatch --rdap-bootstrap http://127.0.0.1:8053/dns.json --ovh-api http://127.0.0.1:8053/v1`. Run the fake registry with `go run ./scripts/fakeregistry --restock-after 1m` to see a VPS come back in stock. For frontend work, also run `cd web && npm run dev` (Vite proxies `/api` to port 8080).

## Rules of the codebase

- **Never send a notification on a failed check.** A lookup error keeps the last known status; only status changes notify (see the table in `docs/DESIGN.md`).
- **Be gentle on registries and OVH.** Domain and VPS checks go through the monitor's single queue, one at a time with a pause in between; do not add parallel or retrying queries. The form's plan and stock lookups (`/api/ovh/*`) are the only OVH requests outside it, made on user action.
- **Never put Apprise URLs on a command line or in logs.** They contain credentials; `notify` passes them in a private temporary file.
- **State changes go through `store.Update`**, which saves before it swaps in the new state. Return `badRequest{err}` from an update function for validation errors.
- **Mutating endpoints** must be POST/PATCH/DELETE (the same-origin check relies on that).
- **Style:** match the surrounding code. Comments explain why, not what. Keep the UI consistent with the existing components (`Modal`, `Menu`, `toasts`, `dialogs`) and the CSS variables in `web/src/app.css`; test both the light and dark themes and a phone-sized viewport.
- **Tests:** add Go tests next to the code. Lookups are tested against `httptest` RDAP servers and local WHOIS listeners, the OVH client against an `httptest` API, Apprise with a stand-in script; tests must not need network access.

## Verifying UI changes

Type checks are not enough. Run the app against the fake registry and drive the changed flow in a browser, for example with Playwright (a dev dependency in `web/`; `web/scripts/screenshots.mjs` shows how). Check desktop (1440×900) and phone (390×844) viewports.

## Screenshots and docs

- When a change affects what the UI looks like, run `make screenshots` and commit the updated `docs/screenshots/*.jpg`. If a flow in `web/scripts/screenshots.mjs` no longer works, update the script. Look at the new images before committing them.
- Keep `README.md` (features, configuration) and `docs/DESIGN.md` (API table, decisions, open items) in sync with the code.

## Git

- CI runs on every push to `main` and on pull requests: tests, formatting, vet, govulncheck and the frontend type check; pushes to `main` publish `ghcr.io/floriandejonckheere/dropcatch:main`.
- Run `make check` and `gofmt -l .` before pushing, and check that the CI run succeeds afterwards.
- Write commit messages that explain what changed and why, with a short subject line.
