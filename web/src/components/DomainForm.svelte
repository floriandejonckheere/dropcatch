<script lang="ts">
  import { untrack } from 'svelte'
  import { api, type Domain } from '../lib/api'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import Modal from './Modal.svelte'
  import OwnInterval from './OwnInterval.svelte'

  let { domain, onclose }: { domain: Domain; onclose: () => void } = $props()

  // Edits a copy, so that cancelling leaves the domain unchanged.
  const initial = untrack(() => $state.snapshot(domain))
  let note = $state(initial.note ?? '')
  let interval = $state(initial.interval ?? 0)
  let error = $state('')
  let busy = $state(false)

  const limits = $derived(session.config?.limits ?? { minInterval: 5, maxInterval: 10080 })

  async function save(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      app.upsert(await api.editDomain(initial.name, { note, interval }))
      toasts.show(`Saved ${initial.display}`, { kind: 'success' })
      onclose()
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<Modal title={`Edit ${initial.display}`} {onclose} width="480px">
  <form id="domain-form" onsubmit={save}>
    <label class="field">
      <span>Note</span>
      <textarea class="input" rows="3" bind:value={note} maxlength="500" placeholder="Optional"></textarea>
    </label>
    <OwnInterval
      bind:value={interval}
      fallback={app.settings.interval}
      min={limits.minInterval}
      max={limits.maxInterval}
      note="For a domain about to drop, check it more often than the others."
    />
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="pill" onclick={onclose}>Cancel</button>
    <button class="pill primary" type="submit" form="domain-form" disabled={busy}>Save</button>
  {/snippet}
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 14px;
  }
</style>
