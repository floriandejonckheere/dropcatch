export interface Toast {
  id: number
  message: string
  kind: 'info' | 'success' | 'error'
  action?: { label: string; run: () => void }
  progress?: number
}

let next = 1

class Toasts {
  items = $state<Toast[]>([])

  show(message: string, opts: Partial<Omit<Toast, 'id' | 'message'>> & { duration?: number } = {}): number {
    const id = next++
    const { duration = opts.kind === 'error' ? 7000 : 4500, ...rest } = opts
    this.items.push({ id, message, kind: 'info', ...rest })
    if (duration > 0) setTimeout(() => this.dismiss(id), duration)
    return id
  }

  error(e: unknown, prefix = '') {
    const msg = e instanceof Error ? e.message : String(e)
    this.show(prefix ? `${prefix}: ${msg}` : msg, { kind: 'error' })
  }

  dismiss(id: number) {
    this.items = this.items.filter((t) => t.id !== id)
  }
}

export const toasts = new Toasts()
