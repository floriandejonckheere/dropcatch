<script lang="ts">
  import type { VPS } from '../lib/api'
  import { relative } from '../lib/format'
  import { app } from '../lib/state.svelte'
  import { city, vpsActions } from '../lib/vps'
  import Icon from './Icon.svelte'
  import { menu } from './Menu.svelte'
  import StatusBadge from './StatusBadge.svelte'
  import VPSDetails from './VPSDetails.svelte'
  import VPSForm from './VPSForm.svelte'

  let { now }: { now: number } = $props()

  // The watch being edited; an empty object adds one.
  let editing = $state<Partial<VPS> | null>(null)
  let details = $state<string | null>(null)
  const selected = $derived(app.vps.find((v) => v.id === details) ?? null)

  // Available first, then by plan name.
  const sorted = $derived(
    [...app.vps].sort(
      (a, b) =>
        Number(b.status === 'available' && !b.paused) - Number(a.status === 'available' && !a.paused) ||
        a.planName.localeCompare(b.planName, undefined, { numeric: true }),
    ),
  )

  /** The watched datacenters, with their stock as of the last check. */
  function watched(v: VPS): { name: string; available: boolean | null }[] {
    if (!v.datacenters.length) return []
    return v.datacenters.map((code) => {
      const s = v.stock?.find((x) => x.code === code)
      return { name: s ? city(s.name) : code, available: s ? s.available : null }
    })
  }

  function inStock(v: VPS): string[] {
    return (v.stock ?? []).filter((s) => s.available && (!v.datacenters.length || v.datacenters.includes(s.code))).map((s) => city(s.name))
  }

  function openMenu(e: MouseEvent, v: VPS) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    menu.show({ clientX: r.right - 200, clientY: r.bottom + 4 }, [
      { label: 'Details', icon: 'info', run: () => (details = v.id) },
      { label: 'Edit', icon: 'rename', run: () => (editing = v) },
      v.paused
        ? { label: 'Resume monitoring', icon: 'play', run: () => vpsActions.setPaused(v, false) }
        : { label: 'Pause monitoring', icon: 'pause', run: () => vpsActions.setPaused(v, true) },
      { label: 'Remove', icon: 'trash', danger: true, separator: true, run: () => vpsActions.remove(v) },
    ])
  }
</script>

<section class="view">
  <header>
    <div>
      <h1>OVHcloud VPS</h1>
      <p class="muted">Get notified when a VPS plan is back in stock in the datacenters you choose.</p>
    </div>
    <button class="pill primary" onclick={() => (editing = {})}><Icon name="plus" size={16} />Watch a VPS</button>
  </header>

  {#if app.vps.length}
    <ul class="list">
      {#each sorted as v (v.id)}
        {@const checking = app.checking[0] === `vps:${v.id}`}
        {@const queued = !checking && app.checking.includes(`vps:${v.id}`)}
        {@const dcs = watched(v)}
        <li class="row card" class:paused={v.paused} class:available={v.status === 'available' && !v.paused}>
          <div class="status"><StatusBadge status={v.status} paused={v.paused} /></div>
          <button class="name" onclick={() => (details = v.id)}>
            <span class="plan ellipsis">{v.planName} <span class="code">{v.plan} · {v.subsidiary}</span></span>
            {#if v.note}<span class="note ellipsis">{v.note}</span>{/if}
          </button>
          <div class="dcs">
            {#if dcs.length}
              {#each dcs as d (d.name)}
                <span class="dc" class:in={d.available} title={d.available === null ? 'Not offered' : d.available ? 'In stock' : 'Out of stock'}>{d.name}</span>
              {/each}
            {:else if inStock(v).length}
              {#each inStock(v) as name (name)}<span class="dc in">{name}</span>{/each}
            {:else}
              <span class="muted any">Any datacenter</span>
            {/if}
          </div>
          <div class="checked">
            {#if checking}
              <span class="muted">Checking…</span>
            {:else if queued}
              <span class="muted">Queued</span>
            {:else if v.error}
              <span class="error" title={v.error}><Icon name="alert" size={14} />Check failed</span>
            {:else if v.checked}
              <span class="muted" title={new Date(v.checked).toLocaleString()}>{relative(v.checked, now)}</span>
            {/if}
          </div>
          <div class="actions">
            <button class="icon-btn" onclick={() => vpsActions.check(v)} disabled={checking || queued} aria-label="Check {v.planName} now" title="Check now">
              <span class="icon" class:spin={checking}><Icon name="refresh" size={17} /></span>
            </button>
            <button class="icon-btn" onclick={(e) => openMenu(e, v)} aria-label="More actions for {v.planName}"><Icon name="more" size={18} /></button>
          </div>
        </li>
      {/each}
    </ul>
  {:else if app.loaded}
    <div class="empty-state card">
      <Icon name="server" size={36} />
      <h2>No VPS watched</h2>
      <p>Pick an OVHcloud VPS plan and the datacenters or regions you want. Dropcatch checks OVH's stock with your domains and notifies you when it can be ordered.</p>
    </div>
  {/if}
</section>

{#if editing}
  <VPSForm watch={editing} onclose={() => (editing = null)} />
{/if}
{#if selected}
  <VPSDetails vps={selected} {now} onclose={() => (details = null)} onedit={() => ((editing = selected), (details = null))} />
{/if}

<style>
  .view {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }

  h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 800;
    letter-spacing: -0.01em;
  }

  header p {
    margin: 4px 0 0;
    font-size: 14px;
  }

  header .pill {
    flex: none;
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
    grid-template-columns: 120px minmax(0, 1.2fr) minmax(0, 1.2fr) 110px auto;
    align-items: center;
    gap: 12px;
    padding: 10px 8px 10px 16px;
  }

  .row.available {
    border-color: color-mix(in srgb, var(--available) 45%, var(--border));
  }

  .row.paused .name,
  .row.paused .dcs {
    opacity: 0.6;
  }

  .name {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
    text-align: left;
  }

  .plan {
    max-width: 100%;
    font-weight: 650;
    font-size: 15px;
  }

  .name:hover .plan {
    color: var(--accent-text);
  }

  .code {
    margin-left: 4px;
    font-weight: 400;
    font-size: 12px;
    color: var(--text-3);
  }

  .note {
    max-width: 100%;
    font-size: 13px;
    color: var(--text-3);
  }

  .dcs {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
  }

  .dc {
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-3);
    background: var(--bg-elev-2);
    white-space: nowrap;
  }

  .dc.in {
    color: var(--available);
    background: color-mix(in srgb, var(--available) 14%, transparent);
  }

  .any {
    font-size: 13px;
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

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 40px 20px;
    text-align: center;
    color: var(--text-3);
  }

  .empty-state h2 {
    margin: 12px 0 0;
    font-size: 17px;
    color: var(--text);
  }

  .empty-state p {
    max-width: 460px;
    margin: 6px 0 0;
    line-height: 1.5;
    color: var(--text-2);
  }

  @media (max-width: 760px) {
    header {
      flex-direction: column;
    }

    .row {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'name actions'
        'status actions'
        'dcs dcs'
        'checked checked';
      row-gap: 6px;
      padding: 12px 8px 12px 14px;
    }

    .status {
      grid-area: status;
    }

    .name {
      grid-area: name;
    }

    .dcs {
      grid-area: dcs;
    }

    .checked {
      grid-area: checked;
      text-align: left;
      font-size: 12px;
    }

    .actions {
      grid-area: actions;
      align-self: start;
    }
  }
</style>
