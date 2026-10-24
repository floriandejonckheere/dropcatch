<script lang="ts">
  import type { VPS } from '../lib/api'
  import { date, dateTime, regionLabel, relative, statusLabel } from '../lib/format'
  import { app } from '../lib/state.svelte'
  import { vpsActions } from '../lib/vps'
  import Icon from './Icon.svelte'
  import Modal from './Modal.svelte'
  import StatusBadge from './StatusBadge.svelte'

  let { vps: v, now, onclose, onedit }: { vps: VPS; now: number; onclose: () => void; onedit: () => void } = $props()

  const checking = $derived(app.checking.includes(`vps:${v.id}`))
  const orderUrl = $derived(
    v.subsidiary === 'US' ? 'https://us.ovhcloud.com/vps/' : `https://www.ovhcloud.com/en/vps/configurator/?planCode=${encodeURIComponent(v.plan)}`,
  )
  const watches = (code: string) => !v.datacenters.length || v.datacenters.includes(code)

  async function remove() {
    if (await vpsActions.remove(v)) onclose()
  }
</script>

<Modal title={v.planName} {onclose} width="580px" focusField={false}>
  <div class="head">
    <StatusBadge status={v.status} paused={v.paused} />
    <span class="muted mono">{v.plan} · {v.subsidiary}</span>
    {#if v.status === 'available'}
      <a class="order" href={orderUrl} target="_blank" rel="noreferrer">Order on OVHcloud <Icon name="external" size={14} /></a>
    {/if}
  </div>

  {#if v.error}
    <p class="error" role="alert"><Icon name="alert" size={16} /><span>Last check failed: {v.error}</span></p>
  {/if}

  <dl>
    <dt>Watching</dt>
    <dd>{v.datacenters.length ? `${v.datacenters.length} datacenter${v.datacenters.length > 1 ? 's' : ''}` : 'Any datacenter'}</dd>
    <dt>Last checked</dt>
    <dd>{checking ? 'Checking now…' : v.checked ? `${dateTime(v.checked)} (${relative(v.checked, now)})` : 'Never'}</dd>
    <dt>Watching since</dt>
    <dd>{date(v.added)}</dd>
    {#if v.note}
      <dt>Note</dt>
      <dd class="note">{v.note}</dd>
    {/if}
  </dl>

  {#if v.stock?.length}
    <h3>Stock</h3>
    <ul class="stock">
      {#each v.stock as s (s.code)}
        <li class:watched={watches(s.code)}>
          <span class="dot" class:in={s.available}></span>
          <span class="dc">{s.name} <span class="muted">· {regionLabel[s.region]}</span></span>
          <span class="state" class:in={s.available}>{s.available ? 'In stock' : 'Out of stock'}</span>
        </li>
      {/each}
    </ul>
    {#if v.datacenters.length}<p class="hint">Datacenters you do not watch are dimmed.</p>{/if}
  {/if}

  {#if v.history?.length}
    <h3>History</h3>
    <ol class="history">
      {#each v.history as h (h.time)}
        <li>
          <span class="muted tabular">{dateTime(h.time)}</span>
          <span>
            {#if h.from === h.to}In stock{:else}{statusLabel[h.from]} → <strong>{statusLabel[h.to]}</strong>{/if}
            {#if h.detail}<span class="muted">: {h.detail}</span>{/if}
          </span>
        </li>
      {/each}
    </ol>
  {/if}

  {#snippet footer()}
    <button class="pill" onclick={remove} aria-label="Remove"><Icon name="trash" size={16} /><span class="wide">Remove</span></button>
    <span class="spacer"></span>
    <button class="pill" onclick={onedit} aria-label="Edit"><Icon name="rename" size={14} /><span class="wide">Edit</span></button>
    <button class="pill" onclick={() => vpsActions.setPaused(v, !v.paused)}>
      <Icon name={v.paused ? 'play' : 'pause'} size={14} />{v.paused ? 'Resume' : 'Pause'}
    </button>
    <button class="pill primary" onclick={() => vpsActions.check(v)} disabled={checking}>
      <span class="icon" class:spin={checking}><Icon name="refresh" size={16} /></span>Check now
    </button>
  {/snippet}
</Modal>

<style>
  .head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-bottom: 12px;
  }

  .mono {
    font-family: var(--mono);
    font-size: 13px;
  }

  .order {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    margin-left: auto;
    font-size: 14px;
    font-weight: 600;
    color: var(--accent-text);
  }

  .error {
    display: flex;
    gap: 8px;
    margin: 0 0 12px;
    padding: 10px 12px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    color: var(--danger);
    font-size: 14px;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }

  .error :global(svg) {
    flex: none;
    margin-top: 1px;
  }

  dl {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 8px 20px;
    margin: 0;
    font-size: 14px;
  }

  dt {
    color: var(--text-3);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .note {
    white-space: pre-wrap;
  }

  h3 {
    margin: 20px 0 8px;
    font-size: 14px;
  }

  .stock,
  .history {
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: 14px;
  }

  .stock li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
    border-top: 1px solid var(--border);
  }

  .stock li:not(.watched) {
    opacity: 0.5;
  }

  .dc {
    flex: 1;
    min-width: 0;
  }

  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--registered);
    opacity: 0.5;
  }

  .dot.in {
    background: var(--available);
    opacity: 1;
  }

  .state {
    font-size: 13px;
    color: var(--text-3);
  }

  .state.in {
    color: var(--available);
    font-weight: 600;
  }

  .hint {
    margin: 6px 0 0;
    font-size: 13px;
    color: var(--text-3);
  }

  .history li {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 7px 0;
    border-top: 1px solid var(--border);
  }

  .history li > span:first-child {
    flex: none;
  }

  .history li > span:last-child {
    text-align: right;
  }

  .spacer {
    flex: 1;
  }

  .icon {
    display: grid;
  }

  @media (max-width: 600px) {
    .wide {
      display: none;
    }

    dl {
      grid-template-columns: 1fr;
      gap: 2px;
    }

    dd {
      margin-bottom: 8px;
    }

    .history li {
      flex-direction: column;
      gap: 2px;
    }

    .history li > span:last-child {
      text-align: left;
    }
  }
</style>
