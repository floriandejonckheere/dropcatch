import { api, type Plan, type Provider, type VPS } from './api'
import { dialogs } from './dialogs.svelte'
import { app } from './state.svelte'
import { toasts } from './toasts.svelte'

// User actions on VPS watches, shared by the list and the details dialog.
export const vpsActions = {
  async check(v: VPS) {
    try {
      await api.checkVPS(v.id)
    } catch (e) {
      toasts.error(e)
    }
  },

  async setPaused(v: VPS, paused: boolean) {
    try {
      app.upsertVPS(await api.editVPS(v.id, { paused }))
      toasts.show(paused ? `Stopped checking ${v.planName}` : `Checking ${v.planName} again`)
    } catch (e) {
      toasts.error(e)
    }
  },

  async remove(v: VPS): Promise<boolean> {
    const ok = await dialogs.confirm({
      title: `Stop watching ${v.planName}?`,
      message: 'Dropcatch stops checking its stock and forgets its history.',
      confirm: 'Remove',
      danger: true,
    })
    if (!ok) return false
    try {
      await api.removeVPS(v.id)
      app.vps = app.vps.filter((x) => x.id !== v.id)
      return true
    } catch (e) {
      toasts.error(e)
      return false
    }
  },
}

export const providerName = (p: Provider) => (p === 'hetzner' ? 'Hetzner Cloud' : 'OVHcloud')

/** What a provider calls the places it runs servers in. */
export const place = (p: Provider) => (p === 'hetzner' ? 'location' : 'datacenter')

/** "3 datacenters", "1 location". */
export const places = (p: Provider, n: number) => `${n} ${place(p)}${n === 1 ? '' : 's'}`

/** "vps-2025-model1 · OVHcloud FR", "cax11 · Hetzner Cloud" */
export const planLine = (v: Pick<VPS, 'provider' | 'plan' | 'subsidiary'>) =>
  v.provider === 'hetzner' ? `${v.plan} · Hetzner Cloud` : `${v.plan} · OVHcloud ${v.subsidiary}`

/** Where the plan can be ordered. */
export function orderUrl(v: VPS): string {
  if (v.provider === 'hetzner') return 'https://console.hetzner.com/'
  if (v.subsidiary === 'US') return 'https://us.ovhcloud.com/vps/'
  return `https://www.ovhcloud.com/en/vps/configurator/?planCode=${encodeURIComponent(v.plan)}`
}

/** "Shared vCPU · Arm64", the group a Hetzner server type is listed in. */
export const family = (p: Plan) => `${p.cpuType === 'dedicated' ? 'Dedicated' : 'Shared'} vCPU · ${p.architecture === 'arm' ? 'Arm64' : 'x86'}`

/** "Gravelines, France" → "Gravelines" for compact lists. */
export const city = (name: string) => name.split(',')[0]

/** "VPS-2 · 4 vCores · 8 GB · 160 GB NVMe · €7.70/month" */
export function planSummary(p: Plan, currency = 'EUR'): string {
  const parts: string[] = []
  if (p.cpuType) parts.push(family(p))
  if (p.cores) parts.push(`${p.cores} ${p.cpuType ? 'vCPUs' : 'vCores'}`)
  if (p.memory) parts.push(`${p.memory} GB RAM`)
  if (p.storage) parts.push(`${p.storage} GB ${p.disk ?? ''}`.trim())
  // Hetzner's prices differ per location.
  if (p.price) parts.push(`${p.cpuType ? 'from ' : ''}${new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(p.price)}/month`)
  return parts.join(' · ')
}
