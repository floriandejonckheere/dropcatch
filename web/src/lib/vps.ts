import { api, type VPS } from './api'
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

/** "Gravelines, France" → "Gravelines" for compact lists. */
export const city = (name: string) => name.split(',')[0]

/** "VPS-2 · 4 vCores · 8 GB · 160 GB NVMe · €7.70/month" */
export function planSummary(p: { cores?: number; memory?: number; storage?: number; disk?: string; price?: number }, currency = 'EUR'): string {
  const parts: string[] = []
  if (p.cores) parts.push(`${p.cores} vCores`)
  if (p.memory) parts.push(`${p.memory} GB RAM`)
  if (p.storage) parts.push(`${p.storage} GB ${p.disk ?? ''}`.trim())
  if (p.price) parts.push(`${new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(p.price)}/month`)
  return parts.join(' · ')
}
