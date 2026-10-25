import { api, type Target } from './api'
import { session } from './session.svelte'

// The service worker and this browser's push subscription. The worker
// also makes the installed app open when the server is unreachable.
class Push {
  registration = $state<ServiceWorkerRegistration | null>(null)
  /** Endpoint of this browser's subscription, to recognise its target. */
  endpoint = $state('')

  async register() {
    if (!('serviceWorker' in navigator)) return
    try {
      this.registration = await navigator.serviceWorker.register('./sw.js', { scope: './' })
      const sub = await this.registration.pushManager?.getSubscription()
      this.endpoint = sub?.endpoint ?? ''
      await this.registerHealthCheck()
    } catch (e) {
      console.warn('service worker:', e)
    }
  }

  // Background checks of the server, where the browser offers them
  // (Chrome, for installed apps); it decides how often they run.
  async registerHealthCheck() {
    const reg = this.registration as (ServiceWorkerRegistration & { periodicSync?: { register(tag: string, o: { minInterval: number }): Promise<void> } }) | null
    if (!reg?.periodicSync) return
    try {
      const state = await navigator.permissions.query({ name: 'periodic-background-sync' as PermissionName })
      if (state.state === 'granted') await reg.periodicSync.register('health', { minInterval: 15 * 60 * 1000 })
    } catch {
      // not supported
    }
  }

  /** Why push cannot be used in this browser, or '' when it can. */
  get unsupported(): string {
    if (!session.config?.pushKey) return 'The server cannot send push notifications.'
    if (!window.isSecureContext) return 'Push notifications need Dropcatch to be served over HTTPS.'
    const ios = /iPhone|iPad|iPod/.test(navigator.userAgent)
    const installed = matchMedia('(display-mode: standalone)').matches
    if (ios && !installed) return 'On iPhone and iPad, add Dropcatch to the home screen first (Share → Add to Home Screen), then open it from there.'
    if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) return 'This browser does not support push notifications.'
    if (Notification.permission === 'denied') return 'Notifications are blocked for Dropcatch; allow them in the browser or system settings.'
    return ''
  }

  /** Subscribes this browser and adds (or renews) its target. */
  async subscribe(name: string): Promise<Target> {
    if ((await Notification.requestPermission()) !== 'granted') throw new Error('Notifications were not allowed.')
    const reg = this.registration ?? (await navigator.serviceWorker.ready)
    let sub = await reg.pushManager.getSubscription()
    if (sub && !sameKey(sub, session.config!.pushKey)) {
      // Subscribed with another server key (the data directory was reset).
      await sub.unsubscribe()
      sub = null
    }
    sub ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: fromBase64(session.config!.pushKey) })
    const json = sub.toJSON()
    const t = await api.addTarget({ name, type: 'push', push: { endpoint: sub.endpoint, p256dh: json.keys!.p256dh, auth: json.keys!.auth }, enabled: true })
    this.endpoint = sub.endpoint
    void this.registerHealthCheck()
    return t
  }

  /** Cancels this browser's subscription, when the target is this browser's. */
  async forget(t: Target) {
    if (!t.push || t.push.endpoint !== this.endpoint) return
    const sub = await this.registration?.pushManager.getSubscription()
    await sub?.unsubscribe()
    this.endpoint = ''
  }
}

function fromBase64(s: string): Uint8Array<ArrayBuffer> {
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/'))
  const out = new Uint8Array(new ArrayBuffer(bin.length))
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function sameKey(sub: PushSubscription, key: string): boolean {
  const k = sub.options.applicationServerKey
  if (!k) return false
  const want = fromBase64(key)
  const got = new Uint8Array(k)
  return got.length === want.length && got.every((b, i) => b === want[i])
}

/** A name for this device, such as "iPhone" or "Chrome on Android". */
export function deviceName(): string {
  const ua = navigator.userAgent
  const os = /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) ? 'iPad' : /Android/.test(ua) ? 'Android' : /Mac/.test(ua) ? 'Mac' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : ''
  if (os === 'iPhone' || os === 'iPad') return os
  const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser'
  return os ? `${browser} on ${os}` : browser
}

export const push = new Push()
