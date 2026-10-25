import { api, url, type Domain, type Settings, type Target, type VPS } from './api'

// The domains, VPS watches, targets and settings, kept current through Server-Sent
// Events (check results, and changes made in other tabs).
class AppState {
  domains = $state<Domain[]>([])
  vps = $state<VPS[]>([])
  targets = $state<Target[]>([])
  settings = $state<Settings>({ interval: 60, stockInterval: 2, timeout: 20, notifyChanges: true })
  checking = $state<string[]>([])
  loaded = $state(false)
  connected = $state(false)
  /** The connection to the server was lost and is being retried. */
  offline = $state(false)
  #source: EventSource | null = null

  async load() {
    const s = await api.state()
    this.domains = s.domains
    this.vps = s.vps
    this.targets = s.targets
    this.settings = s.settings
    this.checking = s.checking
    this.loaded = true
  }

  start() {
    if (this.#source) return
    const es = new EventSource(url('events'))
    this.#source = es
    es.onopen = () => {
      this.connected = true
      this.offline = false
      // Catch up on anything missed while disconnected.
      void this.load()
    }
    es.onerror = () => {
      this.connected = false
      this.offline = true
      // EventSource retries by itself, but gives up when the server, or a
      // proxy in front of it, answers with an error.
      if (es.readyState === EventSource.CLOSED) {
        this.#source = null
        this.#reconnect()
      }
    }
    const on = <T>(type: string, fn: (data: T) => void) =>
      es.addEventListener(type, (e) => fn(JSON.parse((e as MessageEvent).data) as T))
    on<Domain>('domain', (d) => this.upsert(d))
    on<Domain[]>('domains', (ds) => (this.domains = ds))
    on<string>('removed', (name) => (this.domains = this.domains.filter((d) => d.name !== name)))
    on<VPS>('vps', (v) => this.upsertVPS(v))
    on<string>('vpsRemoved', (id) => (this.vps = this.vps.filter((v) => v.id !== id)))
    on<Target[]>('targets', (ts) => (this.targets = ts))
    on<Settings>('settings', (s) => (this.settings = s))
    on<string[]>('checking', (names) => (this.checking = names))
  }

  // Polls the server until it answers, then reconnects.
  #reconnect() {
    setTimeout(async () => {
      try {
        await api.config()
        this.start()
      } catch {
        this.#reconnect()
      }
    }, 10_000)
  }

  upsert(d: Domain) {
    const i = this.domains.findIndex((x) => x.name === d.name)
    if (i >= 0) this.domains[i] = d
    else this.domains = [...this.domains, d].sort((a, b) => a.name.localeCompare(b.name))
  }

  upsertVPS(v: VPS) {
    const i = this.vps.findIndex((x) => x.id === v.id)
    if (i >= 0) this.vps[i] = v
    else this.vps = [...this.vps, v]
  }

  upsertTarget(t: Target) {
    const i = this.targets.findIndex((x) => x.id === t.id)
    if (i >= 0) this.targets[i] = t
    else this.targets = [...this.targets, t]
  }
}

export const app = new AppState()
