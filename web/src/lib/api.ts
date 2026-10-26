export type Status = 'unknown' | 'registered' | 'dropping' | 'available' | 'out-of-stock'

export interface Change {
  time: string
  from: Status
  to: Status
  /** Such as the datacenters with stock of a VPS. */
  detail?: string
}

export interface Domain {
  name: string
  /** Unicode form of an internationalised name. */
  display: string
  note?: string
  /** Own check interval in minutes; absent for the setting. */
  interval?: number
  paused?: boolean
  added: string
  status: Status
  statuses?: string[]
  registrar?: string
  created?: string
  expires?: string
  source?: 'rdap' | 'whois'
  server?: string
  changed?: string
  checked?: string
  error?: string
  history?: Change[]
}

export type Region = 'europe' | 'north-america' | 'asia-pacific' | 'other'

/** Stock of a VPS plan in one datacenter. */
export interface Stock {
  code: string
  name: string
  region: Region
  available: boolean
}

/** A time a datacenter had stock, as seen by the checks. */
export interface Period {
  code: string
  name: string
  start: string
  /** Missing while the stock lasts. */
  end?: string
}

export type Provider = 'ovh' | 'hetzner'

export interface VPS {
  id: string
  provider: Provider
  /** OVH country shop; empty for Hetzner. */
  subsidiary?: string
  plan: string
  planName: string
  /** Watched datacenter (OVH) or location (Hetzner) codes; empty for any. */
  datacenters: string[]
  note?: string
  /** Own check interval in minutes; absent for the setting. */
  interval?: number
  paused?: boolean
  /** Checked, but sends no notifications. */
  muted?: boolean
  added: string
  status: Status
  stock?: Stock[]
  changed?: string
  checked?: string
  error?: string
  history?: Change[]
  /** Newest first, for every datacenter, watched or not. */
  periods?: Period[]
}

export interface Plan {
  code: string
  name: string
  cores?: number
  memory?: number
  storage?: number
  disk?: string
  price?: number
  /** Hetzner: shared or dedicated vCPUs. */
  cpuType?: 'shared' | 'dedicated'
  /** Hetzner: x86 or arm. */
  architecture?: 'x86' | 'arm'
}

export interface Catalog {
  currency: string
  plans: Plan[]
}

export interface Subscription {
  endpoint: string
  p256dh: string
  auth: string
}

export interface Target {
  id: string
  name: string
  /** An Apprise URL, or Web Push to a browser or installed app. */
  type: 'apprise' | 'push'
  url: string
  push?: Subscription
  enabled: boolean
  lastSent?: string
  lastError?: string
}

export interface Settings {
  /** Minutes between checks of each domain. */
  interval: number
  /** Minutes between checks of each VPS. */
  vpsInterval: number
  /** Minutes between checks of a VPS while it is in stock. */
  stockInterval: number
  /** Seconds before a registry query is abandoned. */
  timeout: number
  notifyChanges: boolean
}

export interface State {
  domains: Domain[]
  vps: VPS[]
  targets: Target[]
  settings: Settings
  checking: string[]
}

export interface ServerConfig {
  version: string
  apprise: boolean
  /** VAPID public key for push subscriptions; empty when push is unavailable. */
  pushKey: string
  /** Whether a Hetzner Cloud API token is configured. */
  hetzner: boolean
  ovhSubsidiaries: { code: string; name: string }[]
  limits: {
    minInterval: number
    maxInterval: number
    minVPSInterval: number
    maxVPSInterval: number
    minStockInterval: number
    maxStockInterval: number
    minTimeout: number
    maxTimeout: number
  }
  auth: { required: boolean; username: boolean; authenticated: boolean }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code?: string,
  ) {
    super(message)
  }
}

/** Called when the server reports that the session is missing or expired. */
export const hooks = { unauthorized: () => {} }

async function request<T>(method: string, endpoint: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method }
  if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { 'Content-Type': 'application/json' }
  }
  const res = await fetch(url(endpoint), init)
  if (!res.ok) {
    let data: Record<string, unknown> = {}
    try {
      data = await res.json()
    } catch {
      /* not JSON */
    }
    if (res.status === 401 && data.code === 'unauthorized') hooks.unauthorized()
    throw new ApiError(res.status, (data.error as string) ?? res.statusText, data.code as string | undefined)
  }
  return res.json()
}

/** Builds a URL relative to the page, so Dropcatch works under any base path. */
export function url(endpoint: string): string {
  return `api/${endpoint}`
}

const enc = encodeURIComponent

export const api = {
  config: () => request<ServerConfig>('GET', 'config'),
  login: (username: string, password: string) => request<{ ok: boolean }>('POST', 'login', { username, password }),
  logout: () => request<{ ok: boolean }>('POST', 'logout', {}),

  state: () => request<State>('GET', 'state'),

  addDomains: (names: string, note = '') =>
    request<{ added: Domain[]; failed: { input: string; error: string }[] }>('POST', 'domains', { names, note }),
  /** interval 0 uses the setting for all domains. */
  editDomain: (name: string, patch: { note?: string; paused?: boolean; interval?: number }) => request<Domain>('PATCH', `domains/${enc(name)}`, patch),
  removeDomain: (name: string) => request<{ ok: boolean }>('DELETE', `domains/${enc(name)}`),
  checkDomain: (name: string) => request<{ ok: boolean }>('POST', `domains/${enc(name)}/check`),
  checkAll: () => request<{ ok: boolean }>('POST', 'check'),

  ovhPlans: (subsidiary: string) => request<Catalog>('GET', `ovh/plans?subsidiary=${enc(subsidiary)}`),
  ovhStock: (subsidiary: string, plan: string) =>
    request<{ datacenters: Stock[] }>('GET', `ovh/stock?subsidiary=${enc(subsidiary)}&plan=${enc(plan)}`),
  hetznerPlans: () => request<Catalog>('GET', 'hetzner/plans'),
  hetznerStock: (plan: string) => request<{ datacenters: Stock[] }>('GET', `hetzner/stock?plan=${enc(plan)}`),
  addVPS: (v: { provider: Provider; subsidiary?: string; plan: string; planName: string; datacenters: string[]; note: string; interval?: number; muted?: boolean }) => request<VPS>('POST', 'vps', v),
  /** interval 0 uses the setting for all VPS. */
  editVPS: (id: string, patch: { datacenters?: string[]; note?: string; paused?: boolean; muted?: boolean; interval?: number }) => request<VPS>('PATCH', `vps/${enc(id)}`, patch),
  removeVPS: (id: string) => request<{ ok: boolean }>('DELETE', `vps/${enc(id)}`),
  checkVPS: (id: string) => request<{ ok: boolean }>('POST', `vps/${enc(id)}/check`),

  addTarget: (t: { name: string; enabled: boolean } & ({ type?: 'apprise'; url: string } | { type: 'push'; push: Subscription })) =>
    request<Target>('POST', 'targets', t),
  editTarget: (id: string, patch: Partial<Pick<Target, 'name' | 'url' | 'enabled'>>) => request<Target>('PATCH', `targets/${enc(id)}`, patch),
  removeTarget: (id: string) => request<{ ok: boolean }>('DELETE', `targets/${enc(id)}`),
  testTarget: (id: string) => request<{ ok: boolean }>('POST', `targets/${enc(id)}/test`),

  saveSettings: (s: Partial<Settings>) => request<Settings>('PATCH', 'settings', s),
}
