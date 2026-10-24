import { api, url, type Domain, type Settings, type Target } from './api'

// The domains, targets and settings, kept current through Server-Sent
// Events (check results, and changes made in other tabs).
class AppState {
  domains = $state<Domain[]>([])
  targets = $state<Target[]>([])
  settings = $state<Settings>({ interval: 60, timeout: 20, notifyChanges: true })
  checking = $state<string[]>([])
  loaded = $state(false)
  connected = $state(false)
  #source: EventSource | null = null

  async load() {
    const s = await api.state()
    this.domains = s.domains
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
      // Catch up on anything missed while disconnected.
      void this.load()
    }
    es.onerror = () => (this.connected = false)
    const on = <T>(type: string, fn: (data: T) => void) =>
      es.addEventListener(type, (e) => fn(JSON.parse((e as MessageEvent).data) as T))
    on<Domain>('domain', (d) => this.upsert(d))
    on<Domain[]>('domains', (ds) => (this.domains = ds))
    on<string>('removed', (name) => (this.domains = this.domains.filter((d) => d.name !== name)))
    on<Target[]>('targets', (ts) => (this.targets = ts))
    on<Settings>('settings', (s) => (this.settings = s))
    on<string[]>('checking', (names) => (this.checking = names))
  }

  upsert(d: Domain) {
    const i = this.domains.findIndex((x) => x.name === d.name)
    if (i >= 0) this.domains[i] = d
    else this.domains = [...this.domains, d].sort((a, b) => a.name.localeCompare(b.name))
  }

  upsertTarget(t: Target) {
    const i = this.targets.findIndex((x) => x.id === t.id)
    if (i >= 0) this.targets[i] = t
    else this.targets = [...this.targets, t]
  }
}

export const app = new AppState()
