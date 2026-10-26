<script lang="ts">
  import type { Period, VPS } from '../lib/api'
  import { date, dateTime, duration, minutes } from '../lib/format'
  import { app } from '../lib/state.svelte'
  import { place } from '../lib/vps'

  let { vps: v, now }: { vps: VPS; now: number } = $props()

  type Range = { id: string; label: string; ms: number }
  const hour = 3600000
  const day = 24 * hour
  const ranges: Range[] = [
    { id: 'day', label: '24 hours', ms: day },
    { id: 'week', label: '7 days', ms: 7 * day },
    { id: 'month', label: '30 days', ms: 30 * day },
  ]
  let rangeId = $state('week')
  const range = $derived(ranges.find((r) => r.id === rangeId) ?? ranges[1])
  let expanded = $state(false)
  let hovered = $state<{ period: Period; x: number; lane: number } | null>(null)

  const from = $derived(now - range.ms)
  const where = $derived(place(v.provider))
  const watches = (code: string) => !v.datacenters.length || v.datacenters.includes(code)
  // An open period lasts at least until the last check, which saw the stock.
  const end = (p: Period) => new Date(p.end ?? v.checked ?? p.start).getTime()
  const periods = $derived(v.periods ?? [])
  const visible = $derived(periods.filter((p) => end(p) >= from))

  // One lane per datacenter with stock in the range, watched ones first.
  const lanes = $derived.by(() => {
    const byCode = new Map<string, { code: string; name: string; periods: Period[] }>()
    for (const p of visible) {
      const lane = byCode.get(p.code) ?? { code: p.code, name: p.name, periods: [] }
      lane.periods.push(p)
      byCode.set(p.code, lane)
    }
    return [...byCode.values()].sort((a, b) => Number(watches(b.code)) - Number(watches(a.code)) || a.name.localeCompare(b.name))
  })

  // The end of a period, without its date when that is the start's.
  const until = (start: string, end: string) =>
    date(start) === date(end) ? new Date(end).toLocaleTimeString(undefined, { timeStyle: 'short' }) : dateTime(end)

  const pct = (t: number) => Math.min(100, Math.max(0, ((t - from) / range.ms) * 100))

  // Ticks on local hours and days, stepped in local time so that they stay
  // on the hour across daylight saving changes.
  const ticks = $derived.by(() => {
    const hourly = range.ms === day
    const weekly = range.ms === 30 * day
    const d = new Date(from)
    if (hourly) d.setHours(Math.ceil(d.getHours() / 6) * 6, 0, 0, 0)
    else d.setHours(24, 0, 0, 0)
    const out: { at: number; label: string }[] = []
    for (; d.getTime() < now; hourly ? d.setHours(d.getHours() + 6) : d.setDate(d.getDate() + (weekly ? 7 : 1))) {
      out.push({
        at: pct(d.getTime()),
        label: hourly
          ? d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
          : weekly
            ? d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
            : d.toLocaleDateString(undefined, { weekday: 'short' }),
      })
    }
    return out
  })

  // Time any watched datacenter had stock in the range: the union of the
  // periods, which overlap when several datacenters have stock.
  const total = $derived.by(() => {
    const spans = visible
      .filter((p) => watches(p.code))
      .map((p) => [Math.max(from, new Date(p.start).getTime()), end(p)])
      .sort((a, b) => a[0] - b[0])
    let sum = 0
    let until = -Infinity
    for (const [start, stop] of spans) {
      sum += Math.max(0, stop - Math.max(start, until))
      until = Math.max(until, stop)
    }
    return sum
  })
  const listed = $derived(expanded ? periods : periods.slice(0, 8))
</script>

<div class="head">
  <h3>Stock history</h3>
  <div class="chips" role="tablist" aria-label="Time range">
    {#each ranges as r (r.id)}
      <button class="chip" role="tab" aria-selected={rangeId === r.id} class:active={rangeId === r.id} onclick={() => (rangeId = r.id)}>{r.label}</button>
    {/each}
  </div>
</div>

{#if lanes.length}
  <p class="summary">
    <!-- A period that just started lasts no time yet, but is stock. -->
    {#if visible.some((p) => watches(p.code))}
      In stock for {duration(total)} of the last {range.label}{v.datacenters.length ? ` in the ${where}s you watch` : ''}.
    {:else}
      Not in stock in the {where}s you watch in the last {range.label}.
    {/if}
  </p>
  <div class="chart" role="img" aria-label="Times each {where} had stock; listed in the table below">
    {#each lanes as lane, i (lane.code)}
      <div class="lane" class:dim={!watches(lane.code)}>
        <span class="name" title={lane.name}>{lane.name}</span>
        <div class="track">
          {#each ticks as t (t.at)}<span class="grid" style:left="{t.at}%"></span>{/each}
          {#each lane.periods as p (p.start)}
            {@const left = pct(new Date(p.start).getTime())}
            <span
              class="bar"
              class:open={!p.end}
              style:left="{left}%"
              style:width="{pct(end(p)) - left}%"
              role="presentation"
              onmouseenter={() => (hovered = { period: p, x: (left + pct(end(p))) / 2, lane: i })}
              onmouseleave={() => (hovered = null)}
            ></span>
          {/each}
          {#if hovered && hovered.lane === i}
            {@const p = hovered.period}
            <div class="tip" style:left="{Math.min(80, Math.max(20, hovered.x))}%">
              <strong>{p.name}</strong>
              <span>{dateTime(p.start)} – {p.end ? until(p.start, p.end) : 'now'}</span>
              <span class="muted">{duration(end(p) - new Date(p.start).getTime())}{p.end ? '' : ' so far'}</span>
            </div>
          {/if}
        </div>
      </div>
    {/each}
    <div class="lane axis" aria-hidden="true">
      <span class="name"></span>
      <div class="track">
        {#each ticks as t (t.at)}<span class="tick" style:left="{t.at}%">{t.label}</span>{/each}
      </div>
    </div>
  </div>
{:else}
  <p class="summary">No {where} had stock in the last {range.label}.</p>
{/if}

{#if periods.length}
  <table>
    <thead>
      <tr><th>{where === 'location' ? 'Location' : 'Datacenter'}</th><th>In stock from</th><th>Until</th><th class="num">For</th></tr>
    </thead>
    <tbody>
      {#each listed as p (p.code + p.start)}
        <tr class:dim={!watches(p.code)}>
          <td>{p.name}</td>
          <td class="tabular">{dateTime(p.start)}</td>
          <td class="tabular">{p.end ? dateTime(p.end) : 'Still in stock'}</td>
          <td class="num tabular">{duration(end(p) - new Date(p.start).getTime())}</td>
        </tr>
      {/each}
    </tbody>
  </table>
  {#if periods.length > listed.length}
    <button class="more" onclick={() => (expanded = true)}>Show all {periods.length}</button>
  {/if}
{/if}
<p class="hint">
  Times are those of the checks: every {minutes(Math.min(app.settings.stockInterval, v.interval || app.settings.vpsInterval))} while in stock,
  every {minutes(v.interval || app.settings.vpsInterval)} otherwise.
</p>

<style>
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
    margin: 20px 0 8px;
  }

  h3 {
    margin: 0;
    font-size: 14px;
  }

  .chips {
    display: flex;
    gap: 4px;
  }

  .chip {
    height: 28px;
    padding: 0 10px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-2);
    white-space: nowrap;
  }

  .chip:hover {
    background: var(--bg-hover);
  }

  .chip.active {
    background: var(--bg-hover);
    box-shadow: inset 0 0 0 1px var(--border);
    color: var(--text);
  }

  .summary {
    margin: 0 0 10px;
    font-size: 14px;
    color: var(--text-2);
  }

  .chart {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .lane {
    display: grid;
    grid-template-columns: 130px minmax(0, 1fr);
    align-items: center;
    gap: 10px;
  }

  .lane.dim {
    opacity: 0.5;
  }

  .name {
    font-size: 13px;
    color: var(--text-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .track {
    position: relative;
    height: 18px;
    border-radius: 4px;
    background: var(--bg-elev-2);
  }

  .grid {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: var(--border);
  }

  .bar {
    position: absolute;
    top: 3px;
    bottom: 3px;
    min-width: 3px;
    border-radius: 3px;
    background: var(--available);
  }

  .bar:hover {
    top: 1px;
    bottom: 1px;
  }

  .tip {
    position: absolute;
    bottom: calc(100% + 6px);
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg-elev);
    box-shadow: var(--shadow);
    font-size: 12px;
    white-space: nowrap;
    transform: translateX(-50%);
    pointer-events: none;
  }

  .axis .track {
    height: 16px;
    background: none;
  }

  .tick {
    position: absolute;
    font-size: 11px;
    color: var(--text-3);
    transform: translateX(-50%);
    white-space: nowrap;
  }

  table {
    width: 100%;
    margin-top: 14px;
    border-collapse: collapse;
    font-size: 13px;
  }

  th {
    text-align: left;
    font-weight: 600;
    color: var(--text-3);
    padding: 0 8px 6px 0;
  }

  td {
    padding: 6px 8px 6px 0;
    border-top: 1px solid var(--border);
  }

  .num {
    text-align: right;
    padding-right: 0;
  }

  tr.dim {
    opacity: 0.5;
  }

  .more {
    margin-top: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--accent-text);
  }

  .hint {
    margin: 8px 0 0;
    font-size: 13px;
    color: var(--text-3);
  }

  @media (max-width: 600px) {
    .lane {
      grid-template-columns: minmax(0, 1fr);
      gap: 3px;
    }

    .axis .name {
      display: none;
    }

    th:nth-child(3),
    td:nth-child(3) {
      display: none;
    }
  }
</style>
