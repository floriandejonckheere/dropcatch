// Hash routes, so that reloading works under any base path without server
// support: #/domains, #/notifications, #/settings.

export type View = 'domains' | 'notifications' | 'settings'

const views: View[] = ['domains', 'notifications', 'settings']

function parse(): View {
  const v = location.hash.replace(/^#\/?/, '') as View
  return views.includes(v) ? v : 'domains'
}

class Router {
  view = $state<View>(parse())

  constructor() {
    addEventListener('hashchange', () => (this.view = parse()))
  }

  href(v: View) {
    return `#/${v}`
  }
}

export const router = new Router()
