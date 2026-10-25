<script lang="ts">
  import { untrack } from 'svelte'
  import { api, type Target } from '../lib/api'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import Modal from './Modal.svelte'

  let { target, onclose }: { target: Partial<Target>; onclose: () => void } = $props()

  // Edits a copy, so that cancelling leaves the target unchanged.
  const initial = untrack(() => $state.snapshot(target))
  let name = $state(initial.name ?? '')
  let url = $state(initial.url ?? '')
  let enabled = $state(initial.enabled ?? true)
  let error = $state('')
  let busy = $state(false)

  const isPush = initial.type === 'push'
  const examples = ['ntfy://my-topic', 'tgram://bottoken/chatid', 'discord://webhook_id/webhook_token', 'mailtos://user:password@example.com']

  async function save(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      const t = initial.id
        ? await api.editTarget(initial.id, isPush ? { name, enabled } : { name, url, enabled })
        : await api.addTarget({ name, url, enabled })
      app.upsertTarget(t)
      toasts.show(initial.id ? `Saved ${t.name}` : `Added ${t.name}`, { kind: 'success' })
      onclose()
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<Modal title={initial.id ? `Edit ${initial.name}` : 'Add notification target'} {onclose} width="520px">
  <form id="target-form" onsubmit={save}>
    <label class="field">
      <span>Name</span>
      <input class="input" bind:value={name} placeholder="My phone" maxlength="100" required />
    </label>
    {#if isPush}
      <p class="hint push">Push notifications to a browser or installed app. To move them to another device, enable them there.</p>
    {:else}
      <label class="field">
        <span>Apprise URL</span>
        <input class="input mono" bind:value={url} placeholder="ntfy://my-topic" autocomplete="off" autocapitalize="off" spellcheck="false" required />
      </label>
      <p class="hint">
        For example
        {#each examples as ex, i (ex)}<code>{ex}</code>{i < examples.length - 1 ? ', ' : '.'}{/each}
        See the <a href="https://github.com/caronc/apprise/wiki#notification-services" target="_blank" rel="noreferrer">list of services</a> for the URL of yours.
      </p>
    {/if}
    <label class="check">
      <input type="checkbox" bind:checked={enabled} />
      Send notifications here
    </label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="pill" onclick={onclose}>Cancel</button>
    <button class="pill primary" type="submit" form="target-form" disabled={busy || !name.trim() || (!isPush && !url.trim())}>{busy ? 'Checking…' : 'Save'}</button>
  {/snippet}
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .hint.push {
    margin: 0;
  }

  .mono {
    font-family: var(--mono);
    font-size: 14px;
  }

  .hint {
    margin: -6px 0 0;
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-3);
  }

  code {
    padding: 1px 5px;
    border-radius: 5px;
    background: var(--bg-elev-2);
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  a {
    color: var(--accent-text);
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 14px;
    overflow-wrap: anywhere;
  }
</style>
