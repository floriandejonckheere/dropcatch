import { api, type Domain } from './api'
import { dialogs } from './dialogs.svelte'
import { app } from './state.svelte'
import { toasts } from './toasts.svelte'

// User actions on domains, shared by the list and the details dialog.
export const actions = {
  /** Adds the domains in input; resolves the inputs that were refused. */
  async add(input: string): Promise<string[] | null> {
    try {
      const { added, failed } = await api.addDomains(input)
      // The first check can finish, and arrive as an event, before this
      // response: keep that newer version.
      for (const d of added) if (!app.domains.some((x) => x.name === d.name)) app.upsert(d)
      if (failed.length) {
        const msg = failed.map((f) => `${f.input}: ${f.error}`).join('; ')
        toasts.show(added.length ? `Added ${added.length}, but not ${msg}` : msg, { kind: 'error' })
      } else {
        toasts.show(added.length === 1 ? `Watching ${added[0].display}` : `Watching ${added.length} domains`, { kind: 'success' })
      }
      return failed.map((f) => f.input)
    } catch (e) {
      toasts.error(e)
      return null
    }
  },

  async check(d: Domain) {
    try {
      await api.checkDomain(d.name)
    } catch (e) {
      toasts.error(e)
    }
  },

  async checkAll() {
    try {
      await api.checkAll()
    } catch (e) {
      toasts.error(e)
    }
  },

  async editNote(d: Domain) {
    const note = await dialogs.prompt({ title: `Note for ${d.display}`, label: 'Note', value: d.note ?? '', confirm: 'Save', multiline: true })
    if (note === null) return
    try {
      app.upsert(await api.editDomain(d.name, { note }))
    } catch (e) {
      toasts.error(e)
    }
  },

  async setPaused(d: Domain, paused: boolean) {
    try {
      app.upsert(await api.editDomain(d.name, { paused }))
      toasts.show(paused ? `Stopped checking ${d.display}` : `Checking ${d.display} again`)
    } catch (e) {
      toasts.error(e)
    }
  },

  async remove(d: Domain): Promise<boolean> {
    const ok = await dialogs.confirm({
      title: `Remove ${d.display}?`,
      message: 'Dropcatch stops checking it and forgets its history.',
      confirm: 'Remove',
      danger: true,
    })
    if (!ok) return false
    try {
      await api.removeDomain(d.name)
      app.domains = app.domains.filter((x) => x.name !== d.name)
      return true
    } catch (e) {
      toasts.error(e)
      return false
    }
  },
}
