# Dropcatch — Design

## 1. Summary

Dropcatch is a single binary (shipped as one Docker image) that monitors, for one user, domain names and the stock of OVHcloud VPS plans and Hetzner Cloud server types. It asks each domain's registry, periodically, whether the domain is registered, and OVH's public API or Hetzner's Cloud API whether a server can be ordered in the chosen datacenters or locations, and sends a notification (Web Push or Apprise) when something becomes available. A small web UI manages the domains, the VPS watches, the notification targets and the settings.

It follows the structure of [Wavr](https://github.com/floriandejonckheere/wavr): a Go server with the standard library's HTTP router, a Svelte 5 UI embedded with `embed.FS`, signed session cookies for a single password, configuration through flags and environment variables, and a multi-stage Docker build published to GHCR from CI.

## 2. Stack

| Concern | Choice | Why |
|---|---|---|
| Backend | **Go 1.27**, stdlib `net/http` | One static binary, small memory use, good concurrency for the scheduler and SSE |
| Registry queries | **RDAP** (RFC 9082/9083) with the IANA bootstrap file (RFC 9224), **WHOIS** (RFC 3912) as a fallback | RDAP is structured JSON and mandatory for gTLDs; WHOIS covers the ccTLDs without RDAP |
| Domain names | `golang.org/x/net/idna` and `publicsuffix` | IDN to punycode; reduce subdomains to the registered domain |
| VPS stock | OVH's **public API** (`/vps/order/rule/datacenter`, `/order/catalog/public/vps`); the **Hetzner Cloud API** (`/server_types`, `/locations`) | OVH's needs no account; Hetzner's needs a token from any project, read-only |
| Notifications | **Apprise CLI** and **Web Push** | Apprise: one URL format for over a hundred services, installed with pip in the image. Web Push: notifications on the user's phone without any other service |
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
│ hetzner  Hetzner Cloud server types and stock       │
│ notify   Apprise CLI wrapper                        │
│ store    state in memory, saved to dropcatch.json   │
└───────┬───────────────────────┬────────────────────┘
        │ HTTPS / port 43       │ exec
   registries (RDAP, WHOIS),  apprise → ntfy, Telegram, e-mail, …
   OVH and Hetzner APIs
```

### 3.1 Lookups

- `lookup.Normalize` turns input into the registry's form: it strips a scheme and path, lowercases, converts IDNs to punycode, and reduces the name to one label under its ICANN public suffix (`shop.example.co.uk` → `example.co.uk`). Registries answer "not found" for subdomains, which would otherwise read as available. Private suffixes (`github.io`) are not registries, so `foo.github.io` becomes `github.io`.
- RDAP: the bootstrap file is fetched on first use and refreshed daily; a stale copy is used if IANA is unreachable. The longest matching suffix picks the server, preferring HTTPS URLs. `404` means available, `429` is reported as rate limiting, and a `200` response yields the status codes, the `registration` and `expiration` events, and the registrar's vCard name.
- WHOIS: for TLDs without RDAP, `whois.iana.org` gives the TLD's server (cached for a day). Answers are matched line by line against the phrases registries use for unregistered names ("No match for", "Status: AVAILABLE", "Status: free", …), so that legal notices mentioning "not found" do not count. Anything else is a registration; dates and the registrar are extracted when the format is recognised.
- Statuses: `pending delete`, `redemption period` and `QUARANTINE` (.be) mean **dropping**.

### 3.2 OVHcloud VPS

- A VPS watch is an OVH subsidiary (country shop), a plan code and a set of datacenter codes (`eu-west-gra`); an empty set means any datacenter. The UI groups datacenters by region from the code prefix (`eu` Europe, `ca`/`us` North America, `ap` Asia-Pacific), so a whole region is one click, and the selection stores the individual datacenters.
- The stock comes from `GET /v1/vps/order/rule/datacenter?ovhSubsidiary=FR&planCode=…`, which returns each datacenter offering the plan with `status` and `linuxStatus` (`available` or `out-of-stock`). A datacenter has stock when both are available: the plan can be out of Linux images on its own, and without them the VPS cannot be ordered in a useful form.
- Plans for the form come from `GET /v1/order/catalog/public/vps?ovhSubsidiary=…`: plans whose code starts with `vps` and that carry the `order-funnel:show` tag, sorted by name, with their monthly price (catalog prices are in hundred-millionths) and specifications from the plan's or its product's technical blob. The tag marks what OVH sells to new customers: the catalog also lists retired plans and upgrade paths from older ranges (`vps-essential-2-4-80-vps-2025-model2`), which reuse the plans' names and report stock everywhere but cannot be ordered. OVHcloud US sells one plan under one name in its own datacenters and, as `…-eu` and `…-ca`, in the European and Canadian ones; names shared by several plans get the code appended. The catalog is cached for six hours. If it cannot be loaded, the plan code can be typed in.
- The subsidiary picks the API host: `api.us.ovhcloud.com` for `US` (OVHcloud US, a separate company with its own plans and datacenters), `ca.api.ovh.com` for the Canadian, Asian and World shops, `eu.api.ovh.com` otherwise. `--ovh-api` replaces it (for the fake registry).
- The whole stock (every datacenter offering the plan) is stored, so the details show it and changing the watched datacenters recomputes the status without a query.
- Stock periods: each watch records when each datacenter offering the plan had stock, watched or not, from the first check that saw the stock to the first that did not (the last 100 are kept). The details show them on a timeline of the last 24 hours, 7 days or 30 days, one lane per datacenter, and in a table. A period that is still open is drawn up to the last check, not up to now, since nothing is known after it. Recording unwatched datacenters too means that changing the selection does not lose or invent periods; their precision is that of the regular interval unless a watched datacenter has stock at the same time.

### 3.3 Hetzner Cloud

- A Hetzner watch has the provider `hetzner`, a server type (`cx23`, `cax11`, `ccx13`) as its plan and a set of location codes (`fsn1`, `ash`); there is no subsidiary. The queue, statuses, notifications and stock periods are the same as for OVH: the monitor's `stock` function turns either provider's answer into the stored stock.
- Availability comes from `GET /v1/server_types?name=…`, whose `locations` list each location that offers the type with `available`. Hetzner added that field in April 2026 and removed the datacenter endpoints (which had `server_types.available`) in October 2026. A location whose `deprecation.unavailable_after` has passed is not offered anymore and is left out. Hetzner calls `available` an indication, not a guarantee.
- Location names and regions come from `GET /v1/locations` (cached for a day): the city (without the US state) and country, and the region from the network zone (`eu-central` Europe, `us-east`/`us-west` North America, `ap-southeast` Asia-Pacific), so new locations appear without a code change.
- Plans for the form come from all server types (cached for an hour) that some location still offers, grouped by `cpu_type` (shared, dedicated) and `architecture` (x86, arm) and ordered by series and number within a group (cx22, cx32, then cpx11, …), with the lowest monthly net price over the locations that offer them. Hetzner has no x86 vendor field, so Intel and AMD types are both listed as x86 (amd64).
- The API needs a token (`--hetzner-token`, a deployment secret like the password, with a `-file` variant). Without one, Hetzner watches cannot be added, and existing ones fail their checks with an explanation (and, as any failed check, do not notify). The order link is the Hetzner Console, which has no link to a server type.

### 3.4 Scheduling

- One goroutine checks domains and VPS watches one at a time, with a two-second pause between queries. Registries and OVH rate-limit per client, and watching a few domains and plans does not need parallelism.
- Both share one queue, keyed by the domain name or `vps:<id>` (domain names cannot contain a colon). An item is due when its last check is older than the interval; new ones are due immediately. A VPS watch that is available uses the in-stock interval (a setting, 2 minutes by default, never longer than the regular interval) until a check finds it out of stock: restocks often last minutes, and the shorter interval tells how long they last. Only the watches in stock are checked this often, so the extra queries to OVH stay few. "Check now" and "Check all" put keys in a queue that goes first. The loop sleeps until the next item is due, and wakes early when domains, VPS watches or settings change.
- Each query has a timeout (a setting, 20 seconds by default).

### 3.5 Status changes and notifications

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

### 3.6 Apprise

- The CLI is run once per target. The URL is written to a temporary file (mode 0600) and passed with `--config`, because URLs contain tokens and passwords, and command-line arguments are visible to other users in `ps`.
- `--storage-mode memory`, so Apprise needs no writable home directory.
- Adding or changing a target validates the URL with `apprise --dry-run`, which fails for URLs Apprise cannot parse.
- Without Apprise installed, targets can still be managed; sending reports the missing command and the UI shows a warning.

### 3.7 Web Push and the installed app

- Push targets are browsers that subscribed from the Notifications page. `internal/webpush` encrypts each message for the subscription (RFC 8291, `aes128gcm`) and signs a VAPID token (RFC 8292) with a P-256 key created in the data directory (`vapid.key`, mode 0600), using only the standard library. The public key is in `/api/config`, which the browser needs to subscribe.
- The payload is JSON: title, body, kind, the view to open (`domains`, `vps`) and a tag (the domain or `vps:<id>`), so that a newer message about the same thing replaces the older one on the device. Availability is sent with `Urgency: high`, the rest as normal; the push service keeps messages for a day while the device is offline.
- A browser that subscribes again (notifications turned off and on, a renewed subscription) replaces its target, matched on the endpoint. When the push service answers `404` or `410`, the subscription is gone for good: the target is disabled with an explanation instead of failing on every notification.
- The service worker (`web/public/sw.js`, served unbundled) shows the notifications and opens the right view when one is tapped. It caches the page and its content-hashed assets, network first for the page, so that the installed app opens when the server is down; the page then retries every 10 seconds. The API is never cached.
- Server availability: while the app is open, a lost event stream shows a banner, and the page polls `/api/config` until the server answers, then reconnects (EventSource gives up by itself when a proxy answers with an error). When the app is closed, the only way a browser runs code periodically is periodic background sync, which only Chrome offers, for installed apps, at an interval it picks; the worker uses it to notify when the server stops or starts answering. A reliable outage alert would need a monitor outside Dropcatch.

### 3.8 State

`dropcatch.json` holds the settings, domains, VPS watches and targets (Apprise URLs and push subscriptions). The store keeps the state in memory; `Update` applies a change to a copy, writes it to a temporary file, renames it over the old one, and only then swaps it in, so a failed write leaves both the file and the memory unchanged. The file is created with mode 0600.

### 3.9 Authentication and HTTP security

Taken from Wavr unchanged: a single password (plain or bcrypt) with an optional username; HMAC-signed session cookies (30 days) keyed on a secret stored in the data directory and on the credentials, so changing the password logs out every session; logged-out sessions are remembered until they expire; ten failed logins per client in 15 minutes lock it out for the rest of the window. Modifying requests must be POST, PATCH or DELETE and are refused when their `Origin` is another host. A strict Content Security Policy allows only Dropcatch's own scripts.

## 4. API

All endpoints except `config`, `login` and `logout` require a session when a password is set.

| Method and path | Purpose |
|---|---|
| `GET /api/config` | Version, whether Apprise is installed, the VAPID public key for push, setting limits, login state |
| `POST /api/login`, `POST /api/logout` | Session cookie |
| `GET /api/state` | Domains, VPS watches, targets, settings and the keys being checked |
| `GET /api/events` | SSE: `domain`, `domains`, `removed`, `vps`, `vpsRemoved`, `targets`, `settings`, `checking` |
| `POST /api/domains` | Add domains: `{names, note}`; names are separated by spaces, commas or newlines. Returns `{added, failed}` |
| `PATCH /api/domains/{name}` | Change `note` or `paused` |
| `DELETE /api/domains/{name}` | Stop watching a domain |
| `POST /api/domains/{name}/check` | Check a domain now (queued; the result arrives as an event) |
| `POST /api/check` | Check all active domains and VPS watches now |
| `GET /api/ovh/plans?subsidiary=` | VPS plans of an OVH shop: `{currency, plans}` |
| `GET /api/hetzner/plans` | Hetzner Cloud server types: `{currency, plans}`, with `cpuType` and `architecture` |
| `GET /api/hetzner/stock?plan=` | Locations offering a server type, with their region and current availability |
| `GET /api/ovh/stock?subsidiary=&plan=` | Datacenters offering a plan, with their region and current stock |
| `POST /api/vps` | Watch a VPS: `{provider, subsidiary, plan, planName, datacenters, note}`; `provider` is `ovh` (default) or `hetzner` (no subsidiary, `datacenters` are locations); `datacenters` empty for any. `409` for an identical watch |
| `PATCH /api/vps/{id}` | Change `datacenters`, `note` or `paused` |
| `DELETE /api/vps/{id}` | Stop watching |
| `POST /api/vps/{id}/check` | Check now (queued; the result arrives as an event) |
| `POST /api/targets` | Add a target: `{name, url, enabled}`, or `{name, type: "push", push: {endpoint, p256dh, auth}}` for this browser (replaces its earlier target) |
| `PATCH /api/targets/{id}` | Change `name`, `url` (not for push targets) or `enabled` |
| `DELETE /api/targets/{id}` | Remove a target |
| `POST /api/targets/{id}/test` | Send a test notification; `502` with Apprise's error when it fails |
| `PATCH /api/settings` | Change `interval` (minutes), `stockInterval` (minutes, for VPS in stock), `timeout` (seconds) or `notifyChanges` |

## 5. Decisions

- **No database.** The state of one user fits in a small JSON file, which is easy to back up and inspect.
- **RDAP first, WHOIS as fallback.** RDAP is structured and standard; WHOIS output differs per registry and is only parsed for availability, dates and the registrar.
- **Sequential checks.** Simpler, and gentle on registries and OVH; a few dozen domains take about a minute per round.
- **One schedule for domains and VPS.** Same interval, notification targets and queue; a VPS in stock is checked at a shorter interval until it sells out, rather than all VPS at a separate interval, which would query OVH often for plans that stay out of stock for weeks.
- **OVH's public API, without credentials.** Stock and catalog are public; an OVH API key would add setup for nothing.
- **A token for Hetzner, from the environment.** Hetzner has no public availability endpoint. The token is a deployment secret like the password, so it is a flag or environment variable rather than a setting in the UI and the state file; read-only tokens are enough, as Dropcatch never changes anything in the project.
- **Notify on transitions only.** An available domain is announced once, not on every check.
- **Apprise CLI rather than a library.** Dropcatch stays a Go binary; the image adds Python and Apprise.
- **Web Push implemented in the standard library.** The encryption and VAPID signature are about two hundred lines over `crypto/ecdh`, `crypto/hkdf` and `crypto/ecdsa`, tested against the example in RFC 8291; a dependency would add more code than it saves. Push is a target type rather than an Apprise URL, because the subscription is created by the browser and has to be renewed from it.
- **Hash routes** (`#/notifications`) so that reloading works under any base path.

## 6. Open items

- Registry-specific WHOIS query formats (`.jp` needs `/e`, DENIC offers `-T dn,ace`) are not used; plain queries work for the common ccTLDs.
- A domain that is available on its first check is notified even if the user added it knowing so.
- Per-domain notification targets.
- The OVH response format is not officially documented for these routes; it was taken from OVH's API and community monitors. The order link for OVHcloud US points to the VPS page rather than the configurator.
- Other OVH products (dedicated servers, Kimsufi) have similar availability routes and could be added as watches.
