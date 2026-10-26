<script lang="ts">
  import type { Domain } from '../lib/api'
  import { actions } from '../lib/actions'
  import { date, dateTime, domainEvery, minutes, relative, statusLabel } from '../lib/format'
  import { app } from '../lib/state.svelte'
  import Icon from './Icon.svelte'
  import Modal from './Modal.svelte'
  import StatusBadge from './StatusBadge.svelte'

  let { domain: d, now, onclose, onedit }: { domain: Domain; now: number; onclose: () => void; onedit: () => void } = $props()

  const checking = $derived(app.checking.includes(d.name))

  async function remove() {
    if (await actions.remove(d)) onclose()
  }
</script>

<Modal title={d.display} {onclose} width="560px" focusField={false}>
  <div class="head">
    <StatusBadge status={d.status} paused={d.paused} />
    {#if d.display !== d.name}<span class="muted mono">{d.name}</span>{/if}
  </div>

  {#if d.error}
    <p class="error" role="alert"><Icon name="alert" size={16} /><span>Last check failed: {d.error}</span></p>
  {/if}

  <dl>
    {#if d.registrar}
      <dt>Registrar</dt>
      <dd>{d.registrar}</dd>
    {/if}
    {#if d.created}
      <dt>Registered</dt>
      <dd>{date(d.created)}</dd>
    {/if}
    {#if d.expires}
      <dt>Expires</dt>
      <dd>{date(d.expires)} <span class="muted">({relative(d.expires, now)})</span></dd>
    {/if}
    {#if d.statuses?.length}
      <dt>Registry status</dt>
      <dd class="codes">{#each d.statuses as s (s)}<code>{s}</code>{/each}</dd>
    {/if}
    {#if d.source}
      <dt>Source</dt>
      <dd>{d.source.toUpperCase()} <span class="muted">· {d.server}</span></dd>
    {/if}
    <dt>Last checked</dt>
    <dd>{checking ? 'Checking now…' : d.checked ? dateTime(d.checked) : 'Never'}</dd>
    <dt>Checked every</dt>
    <dd>{minutes(domainEvery(d, app.settings))} <span class="muted">({d.interval ? 'own interval' : 'default'})</span></dd>
    <dt>Watching since</dt>
    <dd>{date(d.added)}</dd>
    {#if d.note}
      <dt>Note</dt>
      <dd class="note">{d.note}</dd>
    {/if}
  </dl>

  {#if d.history?.length}
    <h3>History</h3>
    <ol class="history">
      {#each d.history as h (h.time)}
        <li>
          <span class="muted tabular">{dateTime(h.time)}</span>
          <span>{statusLabel[h.from]} → <strong>{statusLabel[h.to]}</strong></span>
        </li>
      {/each}
    </ol>
  {/if}

  {#snippet footer()}
    <button class="pill" onclick={remove} aria-label="Remove"><Icon name="trash" size={16} /><span class="wide">Remove</span></button>
    <span class="spacer"></span>
    <button class="pill" onclick={onedit} aria-label="Edit"><Icon name="rename" size={14} /><span class="wide">Edit</span></button>
    <button class="pill" onclick={() => actions.setPaused(d, !d.paused)}>
      <Icon name={d.paused ? 'play' : 'pause'} size={14} />{d.paused ? 'Resume' : 'Pause'}
    </button>
    <button class="pill primary" onclick={() => actions.check(d)} disabled={checking}>
      <span class="icon" class:spin={checking}><Icon name="refresh" size={16} /></span>Check now
    </button>
  {/snippet}
</Modal>

<style>
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 12px;
  }

  .mono {
    font-family: var(--mono);
    font-size: 13px;
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

  .codes {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  code {
    padding: 2px 7px;
    border-radius: 6px;
    background: var(--bg-elev-2);
    font-family: var(--mono);
    font-size: 12px;
  }

  h3 {
    margin: 20px 0 8px;
    font-size: 14px;
  }

  .history {
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: 14px;
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
  }
</style>
