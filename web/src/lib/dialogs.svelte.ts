// Promise-based dialogs rendered by DialogHost.

export type Dialog =
  | { kind: 'confirm'; title: string; message: string; confirm: string; danger?: boolean; resolve: (ok: boolean) => void }
  | { kind: 'prompt'; title: string; label: string; value: string; confirm: string; multiline?: boolean; resolve: (value: string | null) => void }

type Without<T> = T extends unknown ? Omit<T, 'kind' | 'resolve'> : never

class Dialogs {
  current = $state<Dialog | null>(null)

  #open<T>(d: Without<Dialog> & { kind: Dialog['kind'] }): Promise<T> {
    return new Promise<T>((resolve) => {
      this.current = {
        ...d,
        resolve: (v: T) => {
          this.current = null
          resolve(v)
        },
      } as unknown as Dialog
    })
  }

  confirm(opts: { title: string; message: string; confirm?: string; danger?: boolean }) {
    return this.#open<boolean>({ kind: 'confirm', confirm: 'OK', ...opts })
  }

  prompt(opts: { title: string; label: string; value?: string; confirm?: string; multiline?: boolean }) {
    return this.#open<string | null>({ kind: 'prompt', value: '', confirm: 'OK', ...opts })
  }
}

export const dialogs = new Dialogs()
