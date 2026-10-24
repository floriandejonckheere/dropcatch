# Dropcatch — Design

## 1. Summary

Dropcatch is a single binary (shipped as one Docker image) that monitors, for one user, domain names and the stock of OVHcloud VPS plans. It asks each domain's registry, periodically, whether the domain is registered, and OVH's public API whether a VPS plan can be ordered in the chosen datacenters, and sends a notification through Apprise when something becomes available. A small web UI manages the domains, the VPS watches, the notification targets and the settings.

It follows the structure of [Wavr](https://github.com/floriandejonckheere/wavr): a Go server with the standard library's HTTP router, a Svelte 5 UI embedded with `embed.FS`, signed session cookies for a single password, configuration through flags and environment variables, and a multi-stage Docker build published to GHCR from CI.

## 2. Stack

| Concern | Choice | Why |
|---|---|---|
| Backend | **Go 1.27**, stdlib `net/http` | One static binary, small memory use, good concurrency for the scheduler and SSE |
| Registry queries | **RDAP** (RFC 9082/9083) with the IANA bootstrap file (RFC 9224), **WHOIS** (RFC 3912) as a fallback | RDAP is structured JSON and mandatory for gTLDs; WHOIS covers the ccTLDs without RDAP |
| Domain names | `golang.org/x/net/idna` and `publicsuffix` | IDN to punycode; reduce subdomains to the registered domain |
| VPS stock | OVH's **public API** (`/vps/order/rule/datacenter`, `/order/catalog/public/vps`) | Needs no OVH account or API keys |
| Notifications | **Apprise CLI** | One URL format for over a hundred services; installed with pip in the image |
| Storage | One **JSON file** in the data directory | A single user has a few dozen domains: no database needed |
| Frontend | **Svelte 5 + Vite**, TypeScript | Same as Wavr; small bundle |
| Live updates | **Server-Sent Events** | Check results arrive in the UI as they happen |
| Container | Multi-stage build; final image `python:3.13-alpine` with Apprise | Apprise is Python |

## 3. Architecture

```
┌──────────────── Browser (Svelte SPA) ─────────────────┐
│ Domains │ VPS │ Details │ Notifications │ Settings     │
└─────┬──────────────────────────┬──────────────────────┘
      │ REST/JSON                │ SSE
┌─────▼──────────────────────────▼──────────────────┐
│ api      auth, same-origin check, handlers, SSE    │
├────────────────────────────────────────────────────┤
│ monitor  scheduler, status changes, notifications  │
│ lookup   RDAP + WHOIS clients, name normalisation   │
│ ovh      OVH VPS stock and plan catalog             │
│ notify   Apprise CLI wrapper                        │
│ store    state in memory, saved to dropcatch.json   │
└───────┬───────────────────────┬────────────────────┘
        │ HTTPS / port 43       │ exec
   registries (RDAP, WHOIS),  apprise → ntfy, Telegram, e-mail, …
   OVH API
```

### 3.1 Lookups

- `lookup.Normalize` turns input into the registry's form: it strips a scheme and path, lowercases, converts IDNs to punycode, and reduces the name to one label under its ICANN public suffix (`shop.example.co.uk` → `example.co.uk`). Registries answer "not found" for subdomains, which would otherwise read as available. Private suffixes (`github.io`) are not registries, so `foo.github.io` becomes `github.io`.
- RDAP: the bootstrap file is fetched on first use and refreshed daily; a stale copy is used if IANA is unreachable. The longest matching suffix picks the server, preferring HTTPS URLs. `404` means available, `429` is reported as rate limiting, and a `200` response yields the status codes, the `registration` and `expiration` events, and the registrar's vCard name.
- WHOIS: for TLDs without RDAP, `whois.iana.org` gives the TLD's server (cached for a day). Answers are matched line by line against the phrases registries use for unregistered names ("No match for", "Status: AVAILABLE", "Status: free", …), so that legal notices mentioning "not found" do not count. Anything else is a registration; dates and the registrar are extracted when the format is recognised.
- Statuses: `pending delete`, `redemption period` and `QUARANTINE` (.be) mean **dropping**.

### 3.2 OVHcloud VPS

- A VPS watch is an OVH subsidiary (country shop), a plan code and a set of datacenter codes (`eu-west-gra`); an empty set means any datacenter. The UI groups datacenters by region from the code prefix (`eu` Europe, `ca`/`us` North America, `ap` Asia-Pacific), so a whole region is one click, and the selection stores the individual datacenters.
- The stock comes from `GET /v1/vps/order/rule/datacenter?ovhSubsidiary=FR&planCode=…`, which returns each datacenter offering the plan with `status` and `linuxStatus` (`available` or `out-of-stock`). A datacenter has stock when both are available: the plan can be out of Linux images on its own, and without them the VPS cannot be ordered in a useful form.
- Plans for the form come from `GET /v1/order/catalog/public/vps?ovhSubsidiary=…`: plans whose code starts with `vps`, with their name, monthly price (catalog prices are in hundred-millionths) and specifications from the plan's or its product's technical blob. The catalog is cached for six hours. If it cannot be loaded, the plan code can be typed in.
- The subsidiary picks the API host: `api.us.ovhcloud.com` for `US` (OVHcloud US, a separate company with its own plans and datacenters), `ca.api.ovh.com` for the Canadian, Asian and World shops, `eu.api.ovh.com` otherwise. `--ovh-api` replaces it (for the fake registry).
- The whole stock (every datacenter offering the plan) is stored, so the details show it and changing the watched datacenters recomputes the status without a query.

### 3.3 Scheduling

- One goroutine checks domains and VPS watches one at a time, with a two-second pause between queries. Registries and OVH rate-limit per client, and watching a few domains and plans does not need parallelism.
- Both share one queue, keyed by the domain name or `vps:<id>` (domain names cannot contain a colon). An item is due when its last check is older than the interval; new ones are due immediately. "Check now" and "Check all" put keys in a queue that goes first. The loop sleeps until the next item is due, and wakes early when domains, VPS watches or settings change.
- Each query has a timeout (a setting, 20 seconds by default).

### 3.4 Status changes and notifications

Domains:

| From → to | Notification |
|---|---|
| any → available (including the first check) | always: "*domain* is available" |
| registered → dropping | when *notify on changes* is on |
| available → registered or dropping | when *notify on changes* is on |
| dropping → registered | when *notify on changes* is on ("restored") |
| not checked → registered or dropping | none |

VPS watches are *not checked*, *out of stock* or *available* (a watched datacenter has stock):

| Change | Notification |
|---|---|
| a watched datacenter gets stock that it did not have (including on the first check) | always: "*plan* is available", with the new datacenters and an order link |
| available → out of stock | when *notify on changes* is on |

So stock appearing in a second datacenter is notified too, while a datacenter selling out while others keep stock is not. Changing the watched datacenters recomputes the status from the last stock without notifying.

A failed check records the error on the domain or VPS and keeps the last known status, so an unreachable registry or API never triggers a notification. Each status change is added to the history (the last 20 are kept); for VPS, a change in which datacenters have stock is recorded too, with their names.

Notifications go to each enabled target separately, so one failing service does not hide the others; the outcome of the last attempt is stored on the target and shown in the UI.

### 3.5 Apprise

- The CLI is run once per target. The URL is written to a temporary file (mode 0600) and passed with `--config`, because URLs contain tokens and passwords, and command-line arguments are visible to other users in `ps`.
- `--storage-mode memory`, so Apprise needs no writable home directory.
- Adding or changing a target validates the URL with `apprise --dry-run`, which fails for URLs Apprise cannot parse.
- Without Apprise installed, targets can still be managed; sending reports the missing command and the UI shows a warning.

### 3.6 State

`dropcatch.json` holds the settings, domains, VPS watches and targets. The store keeps the state in memory; `Update` applies a change to a copy, writes it to a temporary file, renames it over the old one, and only then swaps it in, so a failed write leaves both the file and the memory unchanged. The file is created with mode 0600.

### 3.7 Authentication and HTTP security

Taken from Wavr unchanged: a single password (plain or bcrypt) with an optional username; HMAC-signed session cookies (30 days) keyed on a secret stored in the data directory and on the credentials, so changing the password logs out every session; logged-out sessions are remembered until they expire; ten failed logins per client in 15 minutes lock it out for the rest of the window. Modifying requests must be POST, PATCH or DELETE and are refused when their `Origin` is another host. A strict Content Security Policy allows only Dropcatch's own scripts.

## 4. API

All endpoints except `config`, `login` and `logout` require a session when a password is set.

| Method and path | Purpose |
|---|---|
| `GET /api/config` | Version, whether Apprise is installed, setting limits, login state |
| `POST /api/login`, `POST /api/logout` | Session cookie |
| `GET /api/state` | Domains, VPS watches, targets, settings and the keys being checked |
| `GET /api/events` | SSE: `domain`, `domains`, `removed`, `vps`, `vpsRemoved`, `targets`, `settings`, `checking` |
| `POST /api/domains` | Add domains: `{names, note}`; names are separated by spaces, commas or newlines. Returns `{added, failed}` |
| `PATCH /api/domains/{name}` | Change `note` or `paused` |
| `DELETE /api/domains/{name}` | Stop watching a domain |
| `POST /api/domains/{name}/check` | Check a domain now (queued; the result arrives as an event) |
| `POST /api/check` | Check all active domains and VPS watches now |
| `GET /api/ovh/plans?subsidiary=` | VPS plans of an OVH shop: `{currency, plans}` |
| `GET /api/ovh/stock?subsidiary=&plan=` | Datacenters offering a plan, with their region and current stock |
| `POST /api/vps` | Watch a VPS: `{subsidiary, plan, planName, datacenters, note}`; `datacenters` empty for any. `409` for an identical watch |
| `PATCH /api/vps/{id}` | Change `datacenters`, `note` or `paused` |
| `DELETE /api/vps/{id}` | Stop watching |
| `POST /api/vps/{id}/check` | Check now (queued; the result arrives as an event) |
| `POST /api/targets` | Add a target: `{name, url, enabled}` |
| `PATCH /api/targets/{id}` | Change `name`, `url` or `enabled` |
| `DELETE /api/targets/{id}` | Remove a target |
| `POST /api/targets/{id}/test` | Send a test notification; `502` with Apprise's error when it fails |
| `PATCH /api/settings` | Change `interval` (minutes), `timeout` (seconds) or `notifyChanges` |

## 5. Decisions

- **No database.** The state of one user fits in a small JSON file, which is easy to back up and inspect.
- **RDAP first, WHOIS as fallback.** RDAP is structured and standard; WHOIS output differs per registry and is only parsed for availability, dates and the registrar.
- **Sequential checks.** Simpler, and gentle on registries and OVH; a few dozen domains take about a minute per round.
- **One schedule for domains and VPS.** Same interval, notification targets and queue; a separate interval for VPS can be added if restocks turn out to need faster checks.
- **OVH's public API, without credentials.** Stock and catalog are public; an OVH API key would add setup for nothing.
- **Notify on transitions only.** An available domain is announced once, not on every check.
- **Apprise CLI rather than a library.** Dropcatch stays a Go binary; the image adds Python and Apprise.
- **Hash routes** (`#/notifications`) so that reloading works under any base path.

## 6. Open items

- Registry-specific WHOIS query formats (`.jp` needs `/e`, DENIC offers `-T dn,ace`) are not used; plain queries work for the common ccTLDs.
- A domain that is available on its first check is notified even if the user added it knowing so.
- Per-domain notification targets.
- The OVH response format is not officially documented for these routes; it was taken from OVH's API and community monitors. The order link for OVHcloud US points to the VPS page rather than the configurator.
- Other OVH products (dedicated servers, Kimsufi) have similar availability routes and could be added as watches.
