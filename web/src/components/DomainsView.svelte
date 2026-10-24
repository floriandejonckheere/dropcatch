<script lang="ts">
  import type { Domain, Status } from '../lib/api'
  import { actions } from '../lib/actions'
  import { daysUntil, minutes, relative } from '../lib/format'
  import { app } from '../lib/state.svelte'
  import DomainDetails from './DomainDetails.svelte'
  import Icon from './Icon.svelte'
  import { menu } from './Menu.svelte'
  import StatusBadge from './StatusBadge.svelte'

  let { now }: { now: number } = $props()

  let input = $state('')
  let busy = $state(false)
  let filter = $state<Status | 'all'>('all')
  let query = $state('')
  let details = $state<string | null>(null)

  const counts = $derived(
    app.domains.reduce(
      (c, d) => {
        c[d.status]++
        return c
      },
      { unknown: 0, registered: 0, dropping: 0, available: 0, 'out-of-stock': 0 } as Record<Status, number>,
    ),
  )
  const filters: { id: Status | 'all'; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'available', label: 'Available' },
    { id: 'dropping', label: 'Dropping' },
    { id: 'registered', label: 'Registered' },
  ]
  const shown = $derived(
    app.domains
      .filter(
        (d) =>
          (filter === 'all' || d.status === filter) &&
          (!query || d.display.includes(query.toLowerCase()) || d.name.includes(query.toLowerCase()) || d.note?.toLowerCase().includes(query.toLowerCase())),
      )
      // By the name as shown, not its punycode form.
      .sort((a, b) => a.display.localeCompare(b.display)),
  )
  const selected = $derived(app.domains.find((d) => d.name === details) ?? null)

  async function add(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    const refused = await actions.add(input)
    // Leave what was refused, so that it can be corrected.
    if (refused) input = refused.join(' ')
    busy = false
  }

  function openMenu(e: MouseEvent, d: Domain) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    menu.show({ clientX: r.right - 200, clientY: r.bottom + 4 }, [
      { label: 'Details', icon: 'info', run: () => (details = d.name) },
      { label: 'Edit note', icon: 'rename', run: () => actions.editNote(d) },
      d.paused
        ? { label: 'Resume monitoring', icon: 'play', run: () => actions.setPaused(d, false) }
        : { label: 'Pause monitoring', icon: 'pause', run: () => actions.setPaused(d, true) },
      { label: 'Remove', icon: 'trash', danger: true, separator: true, run: () => actions.remove(d) },
    ])
  }

  function summary(d: Domain): string {
    if (d.status === 'available') return 'Can be registered now'
    if (d.status === 'dropping') return 'Deletion pending'
    if (d.status === 'unknown') return d.error ? '' : 'Waiting for the first check'
    if (!d.expires) return d.registrar ?? ''
    const days = daysUntil(d.expires, now)
    return days < 0 ? `Expired ${relative(d.expires, now)}` : `Expires ${relative(d.expires, now)}`
  }

  function soon(d: Domain): boolean {
    return d.status === 'registered' && !!d.expires && daysUntil(d.expires, now) < 30
  }
</script>

<section class="view">
  <form class="add card" onsubmit={add}>
    <Icon name="globe" size={18} />
    <input
      class="bare"
      bind:value={input}
      placeholder="Add domains, e.g. example.com"
      title="Separate several domains with spaces or commas"
      aria-label="Domains to add"
      autocomplete="off"
      autocapitalize="off"
      spellcheck="false"
    />
    <button class="pill primary" type="submit" disabled={busy || !input.trim()}><Icon name="plus" size={16} />Add</button>
  </form>

  {#if app.domains.length}
    <div class="toolbar">
      <div class="chips" role="tablist" aria-label="Filter by status">
        {#each filters as f (f.id)}
          <button
            class="chip"
            role="tab"
            aria-selected={filter === f.id}
            class:active={filter === f.id}
            onclick={() => (filter = f.id)}
          >
            {f.label}
            <span class="count tabular">{f.id === 'all' ? app.domains.length : counts[f.id]}</span>
          </button>
        {/each}
      </div>
      {#if app.domains.length > 6}
        <label class="search">
          <Icon name="search" size={16} />
          <input class="bare" type="search" bind:value={query} placeholder="Search" aria-label="Search domains" />
        </label>
      {/if}
    </div>

    <ul class="list">
      {#each shown as d (d.name)}
        {@const checking = app.checking[0] === d.name}
        {@const queued = !checking && app.checking.includes(d.name)}
        <li class="row card" class:paused={d.paused} class:available={d.status === 'available' && !d.paused}>
          <div class="status"><StatusBadge status={d.status} paused={d.paused} /></div>
          <button class="name" onclick={() => (details = d.name)}>
            <span class="domain ellipsis">{d.display}</span>
            {#if d.note}<span class="note ellipsis">{d.note}</span>{/if}
          </button>
          <div class="summary ellipsis" class:soon={soon(d)}>
            {summary(d)}
            {#if d.registrar && d.status === 'registered' && d.expires}<span class="muted"> · {d.registrar}</span>{/if}
          </div>
          <div class="checked">
            {#if checking}
              <span class="muted">Checking…</span>
            {:else if queued}
              <span class="muted">Queued</span>
            {:else if d.error}
              <span class="error" title={d.error}><Icon name="alert" size={14} />Check failed</span>
            {:else if d.checked}
              <span class="muted" title={new Date(d.checked).toLocaleString()}>{relative(d.checked, now)}</span>
            {/if}
          </div>
          <div class="actions">
            <button class="icon-btn" onclick={() => actions.check(d)} disabled={checking || queued} aria-label="Check {d.display} now" title="Check now">
              <span class="icon" class:spin={checking}><Icon name="refresh" size={17} /></span>
            </button>
            <button class="icon-btn" onclick={(e) => openMenu(e, d)} aria-label="More actions for {d.display}"><Icon name="more" size={18} /></button>
          </div>
        </li>
      {:else}
        <li class="empty muted">No domains match.</li>
      {/each}
    </ul>
  {:else if app.loaded}
    <div class="empty-state">
      <Icon name="globe" size={40} />
      <h2>Watch a domain</h2>
      <p>Add the domains you want. Dropcatch asks their registry every {minutes(app.settings.interval)} and lets you know when one becomes available.</p>
    </div>
  {/if}
</section>

{#if selected}
  <DomainDetails domain={selected} {now} onclose={() => (details = null)} />
{/if}

<style>
  .view {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .add {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 6px 6px 16px;
    border-radius: 999px;
    color: var(--text-3);
  }

  .add:focus-within {
    border-color: var(--accent);
  }

  .bare {
    flex: 1;
    min-width: 0;
    height: 38px;
    border: 0;
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 15px;
    outline: none;
  }

  .bare::placeholder {
    color: var(--text-3);
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }

  .chips {
    display: flex;
    gap: 6px;
    overflow-x: auto;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 32px;
    padding: 0 12px;
    border-radius: 999px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-2);
    white-space: nowrap;
  }

  .chip:hover {
    background: var(--bg-hover);
  }

  .chip.active {
    background: var(--bg-elev-2);
    color: var(--text);
  }

  .count {
    color: var(--text-3);
    font-weight: 500;
  }

  .search {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    height: 34px;
    border-radius: 999px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    color: var(--text-3);
    width: min(240px, 100%);
  }

  .search .bare {
    height: 32px;
    font-size: 14px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .row {
    display: grid;
    grid-template-columns: 120px minmax(0, 1.4fr) minmax(0, 1fr) 120px auto;
    align-items: center;
    gap: 12px;
    padding: 10px 8px 10px 16px;
    transition: border-color 0.15s;
  }

  .row.available {
    border-color: color-mix(in srgb, var(--available) 45%, var(--border));
  }

  .row.paused .name,
  .row.paused .summary {
    opacity: 0.6;
  }

  .name {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
    text-align: left;
  }

  .domain {
    max-width: 100%;
    font-weight: 650;
    font-size: 15px;
  }

  .name:hover .domain {
    color: var(--accent-text);
  }

  .note {
    max-width: 100%;
    font-size: 13px;
    color: var(--text-3);
  }

  .summary {
    font-size: 14px;
    color: var(--text-2);
  }

  .summary.soon {
    color: var(--dropping);
  }

  .checked {
    font-size: 13px;
    text-align: right;
  }

  .error {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--danger);
    cursor: help;
  }

  .actions {
    display: flex;
  }

  .icon {
    display: grid;
  }

  .empty {
    padding: 24px;
    text-align: center;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 56px 16px;
    text-align: center;
    color: var(--text-3);
  }

  .empty-state h2 {
    margin: 12px 0 0;
    font-size: 18px;
    color: var(--text);
  }

  .empty-state p {
    max-width: 420px;
    margin: 4px 0 0;
    line-height: 1.5;
    color: var(--text-2);
  }

  @media (max-width: 760px) {
    .row {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'name actions'
        'status actions'
        'summary checked';
      row-gap: 6px;
      padding: 12px 8px 12px 14px;
    }

    .status {
      grid-area: status;
    }

    .name {
      grid-area: name;
    }

    .summary {
      grid-area: summary;
      font-size: 13px;
    }

    .checked {
      grid-area: checked;
      font-size: 12px;
    }

    .actions {
      grid-area: actions;
      align-self: start;
    }

    .add .pill {
      padding: 0 14px;
    }
  }
</style>
