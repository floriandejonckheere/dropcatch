// Captures the README screenshots from a running Dropcatch instance that
// uses the fake registry.
//
// Usage: DROPCATCH_URL=http://127.0.0.1:8765/ OUT=../docs/screenshots node scripts/screenshots.mjs
// Normally run through `make screenshots`, which starts the servers first.
import { mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'

const base = process.env.DROPCATCH_URL ?? 'http://127.0.0.1:8765/'
const out = process.env.OUT ?? '../docs/screenshots'
await mkdir(out, { recursive: true })

async function api(method, endpoint, body) {
  const res = await fetch(base + 'api/' + endpoint, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${method} ${endpoint}: ${res.status} ${await res.text()}`)
  return res.json()
}

// Demo data. The fake registry reports names starting with "free" as
// available and those starting with "drop" as pending deletion; of its VPS
// plans, VPS-1 is out of stock and the others are in stock somewhere.
// Watches whose stock is shown in screenshots have the datacenters' names
// once checked. VPS-3 comes from scripts/seed.mjs, with its stock history.
await api('POST', 'domains', { names: 'free-coffee.shop dropshipping.dev example.com pixelforge.io bücher.de northwind.co.uk' })
await api('POST', 'domains', { names: 'free-lunch.app', note: 'For the side project' })
await api('POST', 'domains', { names: 'acme.net', note: 'Ask the owner first' })
await api('PATCH', 'domains/acme.net', { paused: true })
// A domain about to drop is checked more often than the others.
await api('PATCH', 'domains/dropshipping.dev', { interval: 60 })
const eu = ['eu-central-waw', 'eu-south-mil', 'eu-west-gra', 'eu-west-lim', 'eu-west-rbx', 'eu-west-sbg']
await api('POST', 'vps', { subsidiary: 'FR', plan: 'vps-2025-model1', planName: 'VPS-1', datacenters: eu, note: 'Cheapest one', interval: 5 })
await api('POST', 'vps', { subsidiary: 'FR', plan: 'vps-2025-model2', planName: 'VPS-2', datacenters: ['ca-east-bhs'] })
await api('POST', 'vps', { subsidiary: 'FR', plan: 'vps-2025-model4', planName: 'VPS-4', datacenters: ['eu-west-gra', 'eu-west-rbx'], muted: true })
await api('POST', 'vps', { provider: 'hetzner', plan: 'cax11', planName: 'CAX11', datacenters: ['fsn1', 'hel1', 'nbg1'], note: 'Arm build runner' })
await api('POST', 'vps', { provider: 'hetzner', plan: 'ccx13', planName: 'CCX13', datacenters: [] })
await api('POST', 'targets', { name: 'Phone (ntfy)', url: 'ntfy://dropcatch-alerts', enabled: true })
await api('POST', 'targets', { name: 'Telegram', url: 'tgram://123456789:AAF-token/987654', enabled: true })
await api('POST', 'targets', { name: 'E-mail', url: 'mailtos://me:app-password@example.com', enabled: false })
await api('POST', 'targets/' + (await api('GET', 'state')).targets[0].id + '/test')

// Wait for the first checks; the monitor pauses between registry queries.
for (let i = 0; i < 120; i++) {
  const { domains, vps, checking } = await api('GET', 'state')
  if (!checking.length && [...domains, ...vps].every((d) => d.paused || d.checked)) break
  await new Promise((r) => setTimeout(r, 500))
}

// The new headless mode, in which notifications can be allowed, so that
// the Notifications page offers push.
const browser = await chromium.launch(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : { channel: 'chromium' })

async function page(viewport, { mobile = false, scheme = 'dark' } = {}) {
  const ctx = await browser.newContext({
    viewport,
    colorScheme: scheme,
    deviceScaleFactor: mobile ? 2 : 1,
    hasTouch: mobile,
    isMobile: mobile,
    permissions: ['notifications'],
  })
  const p = await ctx.newPage()
  p.on('pageerror', (e) => console.error('page error:', e.message))
  return p
}

const settle = (p, ms = 800) => p.waitForTimeout(ms)
const shot = async (p, name) => {
  await p.evaluate(() => document.activeElement?.blur?.())
  await p.mouse.move(0, 0)
  await settle(p, 200)
  await p.screenshot({ path: `${out}/${name}.jpg`, type: 'jpeg', quality: 85 })
}
const open = async (p, view) => {
  await p.goto(base + '#/' + view)
  await settle(p)
}

const desktop = { width: 1440, height: 900 }
const phone = { width: 390, height: 844 }

let p = await page(desktop)
await open(p, 'domains')
await shot(p, 'domains')

await p.locator('button.name', { hasText: 'dropshipping.dev' }).click()
await settle(p, 400)
await shot(p, 'details')
await p.keyboard.press('Escape')

await open(p, 'vps')
await shot(p, 'vps')

await p.locator('button.name', { hasText: 'VPS-3' }).click()
await settle(p, 400)
await shot(p, 'vps-details')
await p.getByRole('button', { name: 'Close' }).click()
await settle(p, 300)

await p.locator('header button', { hasText: 'Watch a VPS' }).click()
await settle(p, 1200)
await p.locator('select').nth(1).selectOption('vps-2025-model2')
await settle(p, 800)
await p.getByText('Only these:').click()
await p.locator('label.region-name', { hasText: 'Europe' }).locator('input').check()
await shot(p, 'vps-form')
await p.keyboard.press('Escape')

await open(p, 'notifications')
await shot(p, 'notifications')

await open(p, 'settings')
await shot(p, 'settings')

p = await page(desktop, { scheme: 'light' })
await open(p, 'domains')
await shot(p, 'domains-light')

p = await page(phone, { mobile: true })
await open(p, 'vps')
await shot(p, 'mobile-vps')
await open(p, 'domains')
await shot(p, 'mobile-domains')
await p.locator('button.name', { hasText: 'free-lunch.app' }).click()
await settle(p, 400)
await shot(p, 'mobile-details')

await browser.close()
