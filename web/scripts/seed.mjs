// Writes a dropcatch.json with a VPS watch that has a week of stock
// history, which the screenshots could not otherwise show: Dropcatch only
// records the stock it sees.
//
// Usage: node scripts/seed.mjs <data dir>
// Normally run through `make screenshots`, before Dropcatch starts.
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

const dir = process.argv[2]
if (!dir) throw new Error('usage: node scripts/seed.mjs <data dir>')

const now = Date.now()
const ago = (hours) => new Date(now - hours * 3600000).toISOString()
const names = {
  'eu-west-gra': 'Gravelines, France',
  'eu-west-sbg': 'Strasbourg, France',
  'eu-west-lim': 'Limburg, Germany',
  'ap-southeast-sgp': 'Singapore',
}
// [datacenter, start, end] in hours ago; no end while the stock lasts.
// The fake registry has VPS-3 in stock in Strasbourg, Limburg and Singapore.
const periods = [
  ['eu-west-sbg', 3],
  ['eu-west-lim', 9],
  ['eu-west-sbg', 20, 17.5],
  ['eu-west-gra', 31, 30.6],
  ['ap-southeast-sgp', 52],
  ['eu-west-sbg', 70, 61],
  ['eu-west-gra', 98, 97.8],
  ['eu-west-lim', 120, 104],
  ['eu-west-sbg', 145, 142],
].map(([code, start, end]) => ({ code, name: names[code], start: ago(start), ...(end ? { end: ago(end) } : {}) }))

const state = {
  settings: { interval: 720, vpsInterval: 15, stockInterval: 2, timeout: 20, notifyChanges: true },
  domains: [],
  vps: [
    {
      id: 'a1b2c3d4e5f6',
      subsidiary: 'FR',
      plan: 'vps-2025-model3',
      planName: 'VPS-3',
      datacenters: [],
      added: ago(24 * 9),
      status: 'available',
      changed: ago(52),
      checked: ago(1),
      history: [
        { time: ago(9), from: 'available', to: 'available', detail: 'Limburg, Germany; Singapore' },
        { time: ago(52), from: 'out-of-stock', to: 'available', detail: 'Singapore' },
        { time: ago(61), from: 'available', to: 'out-of-stock' },
        { time: ago(70), from: 'out-of-stock', to: 'available', detail: 'Strasbourg, France' },
        { time: ago(24 * 9), from: 'unknown', to: 'out-of-stock' },
      ],
      periods,
    },
  ],
  targets: [],
}

await mkdir(dir, { recursive: true, mode: 0o700 })
await writeFile(join(dir, 'dropcatch.json'), JSON.stringify(state, null, 2) + '\n', { mode: 0o600 })
