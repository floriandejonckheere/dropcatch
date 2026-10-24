export type Status = 'unknown' | 'registered' | 'dropping' | 'available'

export interface Change {
  time: string
  from: Status
  to: Status
}

export interface Domain {
  name: string
  /** Unicode form of an internationalised name. */
  display: string
  note?: string
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

export interface Target {
  id: string
  name: string
  url: string
  enabled: boolean
  lastSent?: string
  lastError?: string
}

export interface Settings {
  /** Minutes between checks of each domain. */
  interval: number
  /** Seconds before a registry query is abandoned. */
  timeout: number
  notifyChanges: boolean
}

export interface State {
  domains: Domain[]
  targets: Target[]
  settings: Settings
  checking: string[]
}

export interface ServerConfig {
  version: string
  apprise: boolean
  limits: { minInterval: number; maxInterval: number; minTimeout: number; maxTimeout: number }
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
  editDomain: (name: string, patch: { note?: string; paused?: boolean }) => request<Domain>('PATCH', `domains/${enc(name)}`, patch),
  removeDomain: (name: string) => request<{ ok: boolean }>('DELETE', `domains/${enc(name)}`),
  checkDomain: (name: string) => request<{ ok: boolean }>('POST', `domains/${enc(name)}/check`),
  checkAll: () => request<{ ok: boolean }>('POST', 'check'),

  addTarget: (t: Omit<Target, 'id'>) => request<Target>('POST', 'targets', { name: t.name, url: t.url, enabled: t.enabled }),
  editTarget: (id: string, patch: Partial<Pick<Target, 'name' | 'url' | 'enabled'>>) => request<Target>('PATCH', `targets/${enc(id)}`, patch),
  removeTarget: (id: string) => request<{ ok: boolean }>('DELETE', `targets/${enc(id)}`),
  testTarget: (id: string) => request<{ ok: boolean }>('POST', `targets/${enc(id)}/test`),

  saveSettings: (s: Partial<Settings>) => request<Settings>('PATCH', 'settings', s),
}
