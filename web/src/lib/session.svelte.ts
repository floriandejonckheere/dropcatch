import { api, hooks, type ServerConfig } from './api'

class Session {
  config = $state<ServerConfig | null>(null)
  authenticated = $state(true)
  error = $state('')
  #retry: ReturnType<typeof setTimeout> | undefined

  constructor() {
    hooks.unauthorized = () => (this.authenticated = false)
  }

  async load() {
    try {
      this.config = await api.config()
      this.authenticated = this.config.auth.authenticated
      this.error = ''
    } catch (e) {
      // fetch fails with a TypeError when the server cannot be reached.
      this.error = e instanceof TypeError || !(e as Error).message ? 'Cannot reach the Dropcatch server.' : (e as Error).message
      // Such as when the installed app opens while the server is down.
      clearTimeout(this.#retry)
      this.#retry = setTimeout(() => this.load(), 10_000)
    }
  }

  async login(username: string, password: string) {
    await api.login(username, password)
    await this.load()
  }

  async logout() {
    await api.logout()
    location.reload()
  }
}

export const session = new Session()
