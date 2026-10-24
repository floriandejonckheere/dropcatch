import { api, hooks, type ServerConfig } from './api'

class Session {
  config = $state<ServerConfig | null>(null)
  authenticated = $state(true)
  error = $state('')

  constructor() {
    hooks.unauthorized = () => (this.authenticated = false)
  }

  async load() {
    try {
      this.config = await api.config()
      this.authenticated = this.config.auth.authenticated
      this.error = ''
    } catch (e) {
      this.error = (e as Error).message || 'Cannot reach the Dropcatch server.'
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
