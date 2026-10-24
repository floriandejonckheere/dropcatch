# Dropcatch — Design

## 1. Summary

Dropcatch is a single binary (shipped as one Docker image) that monitors domain names for one user. It asks each domain's registry, periodically, whether the domain is registered, and sends a notification through Apprise when one becomes available. A small web UI manages the domains, the notification targets and the settings.

It follows the structure of [Wavr](https://github.com/floriandejonckheere/wavr): a Go server with the standard library's HTTP router, a Svelte 5 UI embedded with `embed.FS`, signed session cookies for a single password, configuration through flags and environment variables, and a multi-stage Docker build published to GHCR from CI.

## 2. Stack

| Concern | Choice | Why |
|---|---|---|
| Backend | **Go 1.27**, stdlib `net/http` | One static binary, small memory use, good concurrency for the scheduler and SSE |
| Registry queries | **RDAP** (RFC 9082/9083) with the IANA bootstrap file (RFC 9224), **WHOIS** (RFC 3912) as a fallback | RDAP is structured JSON and mandatory for gTLDs; WHOIS covers the ccTLDs without RDAP |
| Domain names | `golang.org/x/net/idna` and `publicsuffix` | IDN to punycode; reduce subdomains to the registered domain |
| Notifications | **Apprise CLI** | One URL format for over a hundred services; installed with pip in the image |
| Storage | One **JSON file** in the data directory | A single user has a few dozen domains: no database needed |
| Frontend | **Svelte 5 + Vite**, TypeScript | Same as Wavr; small bundle |
| Live updates | **Server-Sent Events** | Check results arrive in the UI as they happen |
| Container | Multi-stage build; final image `python:3.13-alpine` with Apprise | Apprise is Python |

## 3. Architecture

```
┌────────────── Browser (Svelte SPA) ───────────────┐
│ Domains │ Domain details │ Notifications │ Settings │
└─────┬──────────────────────────┬──────────────────┘
      │ REST/JSON                │ SSE
┌─────▼──────────────────────────▼──────────────────┐
│ api      auth, same-origin check, handlers, SSE    │
├────────────────────────────────────────────────────┤
│ monitor  scheduler, status changes, notifications  │
│ lookup   RDAP + WHOIS clients, name normalisation   │
│ notify   Apprise CLI wrapper                        │
│ store    state in memory, saved to dropcatch.json   │
└───────┬───────────────────────┬────────────────────┘
        │ HTTPS / port 43       │ exec
   registries (RDAP, WHOIS)   apprise → ntfy, Telegram, e-mail, …
```

### 3.1 Lookups

- `lookup.Normalize` turns input into the registry's form: it strips a scheme and path, lowercases, converts IDNs to punycode, and reduces the name to one label under its ICANN public suffix (`shop.example.co.uk` → `example.co.uk`). Registries answer "not found" for subdomains, which would otherwise read as available. Private suffixes (`github.io`) are not registries, so `foo.github.io` becomes `github.io`.
- RDAP: the bootstrap file is fetched on first use and refreshed daily; a stale copy is used if IANA is unreachable. The longest matching suffix picks the server, preferring HTTPS URLs. `404` means available, `429` is reported as rate limiting, and a `200` response yields the status codes, the `registration` and `expiration` events, and the registrar's vCard name.
- WHOIS: for TLDs without RDAP, `whois.iana.org` gives the TLD's server (cached for a day). Answers are matched line by line against the phrases registries use for unregistered names ("No match for", "Status: AVAILABLE", "Status: free", …), so that legal notices mentioning "not found" do not count. Anything else is a registration; dates and the registrar are extracted when the format is recognised.
- Statuses: `pending delete`, `redemption period` and `QUARANTINE` (.be) mean **dropping**.

### 3.2 Scheduling

- One goroutine checks domains one at a time, with a two-second pause between queries. Registries rate-limit per client, and drop-catching a few domains does not need parallelism.
- A domain is due when its last check is older than the interval; new domains are due immediately. "Check now" and "Check all" put domains in a queue that goes first. The loop sleeps until the next domain is due, and wakes early when domains or settings change.
- Each query has a timeout (a setting, 20 seconds by default).

### 3.3 Status changes and notifications

| From → to | Notification |
|---|---|
| any → available (including the first check) | always: "*domain* is available" |
| registered → dropping | when *notify on changes* is on |
| available → registered or dropping | when *notify on changes* is on |
| dropping → registered | when *notify on changes* is on ("restored") |
| not checked → registered or dropping | none |

A failed check records the error on the domain and keeps the last known status, so an unreachable registry never triggers a notification. Each status change is added to the domain's history (the last 20 are kept).

Notifications go to each enabled target separately, so one failing service does not hide the others; the outcome of the last attempt is stored on the target and shown in the UI.

### 3.4 Apprise

- The CLI is run once per target. The URL is written to a temporary file (mode 0600) and passed with `--config`, because URLs contain tokens and passwords, and command-line arguments are visible to other users in `ps`.
- `--storage-mode memory`, so Apprise needs no writable home directory.
- Adding or changing a target validates the URL with `apprise --dry-run`, which fails for URLs Apprise cannot parse.
- Without Apprise installed, targets can still be managed; sending reports the missing command and the UI shows a warning.

### 3.5 State

`dropcatch.json` holds the settings, domains and targets. The store keeps the state in memory; `Update` applies a change to a copy, writes it to a temporary file, renames it over the old one, and only then swaps it in, so a failed write leaves both the file and the memory unchanged. The file is created with mode 0600.

### 3.6 Authentication and HTTP security

Taken from Wavr unchanged: a single password (plain or bcrypt) with an optional username; HMAC-signed session cookies (30 days) keyed on a secret stored in the data directory and on the credentials, so changing the password logs out every session; logged-out sessions are remembered until they expire; ten failed logins per client in 15 minutes lock it out for the rest of the window. Modifying requests must be POST, PATCH or DELETE and are refused when their `Origin` is another host. A strict Content Security Policy allows only Dropcatch's own scripts.

## 4. API

All endpoints except `config`, `login` and `logout` require a session when a password is set.

| Method and path | Purpose |
|---|---|
| `GET /api/config` | Version, whether Apprise is installed, setting limits, login state |
| `POST /api/login`, `POST /api/logout` | Session cookie |
| `GET /api/state` | Domains, targets, settings and the domains being checked |
| `GET /api/events` | SSE: `domain`, `domains`, `removed`, `targets`, `settings`, `checking` |
| `POST /api/domains` | Add domains: `{names, note}`; names are separated by spaces, commas or newlines. Returns `{added, failed}` |
| `PATCH /api/domains/{name}` | Change `note` or `paused` |
| `DELETE /api/domains/{name}` | Stop watching a domain |
| `POST /api/domains/{name}/check` | Check a domain now (queued; the result arrives as an event) |
| `POST /api/check` | Check all active domains now |
| `POST /api/targets` | Add a target: `{name, url, enabled}` |
| `PATCH /api/targets/{id}` | Change `name`, `url` or `enabled` |
| `DELETE /api/targets/{id}` | Remove a target |
| `POST /api/targets/{id}/test` | Send a test notification; `502` with Apprise's error when it fails |
| `PATCH /api/settings` | Change `interval` (minutes), `timeout` (seconds) or `notifyChanges` |

## 5. Decisions

- **No database.** The state of one user fits in a small JSON file, which is easy to back up and inspect.
- **RDAP first, WHOIS as fallback.** RDAP is structured and standard; WHOIS output differs per registry and is only parsed for availability, dates and the registrar.
- **Sequential checks.** Simpler, and gentle on registries; a few dozen domains take about a minute per round.
- **Notify on transitions only.** An available domain is announced once, not on every check.
- **Apprise CLI rather than a library.** Dropcatch stays a Go binary; the image adds Python and Apprise.
- **Hash routes** (`#/notifications`) so that reloading works under any base path.

## 6. Open items

- Registry-specific WHOIS query formats (`.jp` needs `/e`, DENIC offers `-T dn,ace`) are not used; plain queries work for the common ccTLDs.
- A domain that is available on its first check is notified even if the user added it knowing so.
- Per-domain notification targets.
