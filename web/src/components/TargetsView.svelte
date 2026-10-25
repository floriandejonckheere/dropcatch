<script lang="ts">
  import { api, type Target } from '../lib/api'
  import { dialogs } from '../lib/dialogs.svelte'
  import { maskUrl, relative } from '../lib/format'
  import { deviceName, push } from '../lib/push.svelte'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import Icon from './Icon.svelte'
  import { menu } from './Menu.svelte'
  import TargetForm from './TargetForm.svelte'

  let { now }: { now: number } = $props()

  // The target being edited; an empty object adds one.
  let editing = $state<Partial<Target> | null>(null)
  let testing = $state<string | null>(null)
  let subscribing = $state(false)

  const thisDevice = (t: Target) => !!push.endpoint && t.push?.endpoint === push.endpoint
  const subscribed = $derived(app.targets.some((t) => thisDevice(t) && t.enabled))
  const host = (t: Target) => {
    try {
      return new URL(t.push!.endpoint).host
    } catch {
      return ''
    }
  }

  async function subscribe() {
    subscribing = true
    try {
      const existing = app.targets.find(thisDevice)
      const t = await push.subscribe(existing?.name ?? deviceName())
      app.upsertTarget(t)
      toasts.show(`${t.name} gets push notifications now`, { kind: 'success' })
    } catch (e) {
      toasts.error(e)
    } finally {
      subscribing = false
    }
  }

  async function test(t: Target) {
    testing = t.id
    try {
      await api.testTarget(t.id)
      toasts.show(`Test notification sent to ${t.name}`, { kind: 'success' })
    } catch (e) {
      toasts.error(e, t.name)
    } finally {
      testing = null
    }
  }

  async function toggle(t: Target) {
    try {
      app.upsertTarget(await api.editTarget(t.id, { enabled: !t.enabled }))
    } catch (e) {
      toasts.error(e)
    }
  }

  async function remove(t: Target) {
    if (!(await dialogs.confirm({ title: `Remove ${t.name}?`, message: 'Notifications will no longer be sent there.', confirm: 'Remove', danger: true }))) return
    try {
      await api.removeTarget(t.id)
      app.targets = app.targets.filter((x) => x.id !== t.id)
      await push.forget(t).catch(() => {})
    } catch (e) {
      toasts.error(e)
    }
  }

  function openMenu(e: MouseEvent, t: Target) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    menu.show({ clientX: r.right - 200, clientY: r.bottom + 4 }, [
      { label: 'Edit', icon: 'rename', run: () => (editing = t) },
      { label: 'Send test', icon: 'send', run: () => test(t) },
      { label: 'Remove', icon: 'trash', danger: true, separator: true, run: () => remove(t) },
    ])
  }
</script>

<section class="view">
  <header>
    <div>
      <h1>Notifications</h1>
      <p class="muted">When a domain or VPS becomes available, every enabled target is notified.</p>
    </div>
    <button class="pill primary" onclick={() => (editing = {})}><Icon name="plus" size={16} />Add target</button>
  </header>

  {#if session.config && !session.config.apprise}
    <p class="warning" role="alert">
      <Icon name="alert" size={16} />
      <span>The <code>apprise</code> command is not installed, so notifications cannot be sent to Apprise URLs. The Docker image includes it.</span>
    </p>
  {/if}

  {#if !subscribed && session.config?.pushKey}
    <div class="device card">
      <Icon name="bell" size={20} />
      <div>
        <strong>Push notifications on this device</strong>
        <p>{push.unsupported || 'Get a notification on this phone or computer when something becomes available, even when Dropcatch is closed.'}</p>
      </div>
      {#if !push.unsupported}
        <button class="pill primary" onclick={subscribe} disabled={subscribing}>{subscribing ? 'Enabling…' : 'Enable'}</button>
      {/if}
    </div>
  {/if}

  {#if app.targets.length}
    <ul class="list">
      {#each app.targets as t (t.id)}
        <li class="row card" class:off={!t.enabled}>
          <label class="switch" title={t.enabled ? 'Enabled' : 'Disabled'}>
            <input type="checkbox" role="switch" checked={t.enabled} onchange={() => toggle(t)} aria-label="Send notifications to {t.name}" />
            <span class="track"></span>
          </label>
          <button class="info" onclick={() => (editing = t)}>
            <span class="name ellipsis">{t.name}{#if thisDevice(t)}<span class="tag">This device</span>{/if}</span>
            {#if t.type === 'push'}
              <span class="url ellipsis">Push · {host(t)}</span>
            {:else}
              <span class="url ellipsis">{maskUrl(t.url)}</span>
            {/if}
          </button>
          <div class="last">
            {#if t.lastError}
              <span class="error" title={t.lastError}><Icon name="alert" size={14} />Failed {relative(t.lastSent, now)}</span>
            {:else if t.lastSent}
              <span class="muted">Sent {relative(t.lastSent, now)}</span>
            {:else}
              <span class="muted">Nothing sent yet</span>
            {/if}
          </div>
          <div class="actions">
            <button class="icon-btn" onclick={() => test(t)} disabled={testing === t.id} aria-label="Send a test notification to {t.name}" title="Send test">
              <Icon name="send" size={17} />
            </button>
            <button class="icon-btn" onclick={(e) => openMenu(e, t)} aria-label="More actions for {t.name}"><Icon name="more" size={18} /></button>
          </div>
        </li>
      {/each}
    </ul>
  {:else if app.loaded}
    <div class="empty-state card">
      <Icon name="bell" size={36} />
      <h2>No notification targets</h2>
      <p>
        Enable push notifications on your phone, or add an <a href="https://github.com/caronc/apprise/wiki" target="_blank" rel="noreferrer">Apprise URL</a> for
        each place you want to be told: ntfy, Telegram, Discord, Slack, e-mail, Pushover, Gotify and over a hundred more.
      </p>
    </div>
  {/if}
</section>

{#if editing}
  <TargetForm target={editing} onclose={() => (editing = null)} />
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

  .warning {
    display: flex;
    gap: 10px;
    margin: 0;
    padding: 12px 14px;
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--dropping) 14%, transparent);
    color: var(--dropping);
    font-size: 14px;
    line-height: 1.45;
  }

  .warning :global(svg) {
    flex: none;
    margin-top: 2px;
  }

  .device {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 16px;
  }

  .device > :global(svg) {
    flex: none;
    color: var(--accent-text);
  }

  .device div {
    flex: 1;
    min-width: 0;
  }

  .device strong {
    font-size: 14px;
  }

  .device p {
    margin: 2px 0 0;
    font-size: 13px;
    line-height: 1.45;
    color: var(--text-2);
  }

  .device .pill {
    flex: none;
  }

  .tag {
    margin-left: 8px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--bg-elev-2);
    font-size: 11px;
    font-weight: 600;
    color: var(--text-2);
    vertical-align: 1px;
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
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 10px 8px 10px 16px;
  }

  .row.off .info {
    opacity: 0.55;
  }

  .info {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
    text-align: left;
  }

  .name {
    max-width: 100%;
    font-weight: 650;
  }

  .info:hover .name {
    color: var(--accent-text);
  }

  .url {
    max-width: 100%;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-3);
  }

  .last {
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

  .switch {
    position: relative;
    display: inline-block;
    width: 38px;
    height: 22px;
    cursor: pointer;
  }

  .switch input {
    position: absolute;
    opacity: 0;
    inset: 0;
    margin: 0;
    cursor: pointer;
  }

  .track {
    position: absolute;
    inset: 0;
    border-radius: 999px;
    background: var(--bg-hover);
    border: 1px solid var(--border);
    transition: background 0.15s;
  }

  .track::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--text-2);
    transition: transform 0.15s, background 0.15s;
  }

  .switch input:checked + .track {
    background: var(--accent);
    border-color: var(--accent);
  }

  .switch input:checked + .track::after {
    transform: translateX(16px);
    background: #fff;
  }

  .switch input:focus-visible + .track {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
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
    max-width: 440px;
    margin: 6px 0 0;
    line-height: 1.5;
    color: var(--text-2);
  }

  .empty-state a,
  .warning code {
    color: var(--accent-text);
  }

  @media (max-width: 600px) {
    header {
      flex-direction: column;
    }

    .row {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'switch info actions'
        '. last actions';
      row-gap: 4px;
    }

    .switch {
      grid-area: switch;
    }

    .info {
      grid-area: info;
    }

    .last {
      grid-area: last;
      text-align: left;
      font-size: 12px;
    }

    .actions {
      grid-area: actions;
    }
  }
</style>
