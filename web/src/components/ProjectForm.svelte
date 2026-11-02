<script lang="ts">
  import { untrack } from 'svelte'
  import { api, type Project } from '../lib/api'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import Modal from './Modal.svelte'

  let { project, onclose }: { project: Partial<Project>; onclose: () => void } = $props()

  const initial = untrack(() => $state.snapshot(project))
  const editing = !!initial.id
  let name = $state(initial.name ?? '')
  // The saved token is never sent back: empty keeps it.
  let token = $state('')
  let error = $state('')
  let busy = $state(false)

  async function save(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      if (editing) {
        await api.editProject(initial.id!, token.trim() ? { name, token } : { name })
      } else {
        const p = await api.addProject({ name, token })
        app.projects = [...app.projects.filter((x) => x.id !== p.id), p]
      }
      toasts.show(editing ? `Saved ${name.trim()}` : `Added ${name.trim()}`, { kind: 'success' })
      onclose()
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<Modal title={editing ? `Edit ${initial.name}` : 'Add Hetzner project'} {onclose} width="520px">
  <form id="project-form" onsubmit={save}>
    <label class="field">
      <span>Name</span>
      <input class="input" bind:value={name} placeholder="Production" maxlength="100" required />
    </label>
    <label class="field">
      <span>API token</span>
      <input
        class="input mono"
        type="password"
        bind:value={token}
        placeholder={editing ? `Unchanged (${initial.token})` : ''}
        autocomplete="off"
        spellcheck="false"
        required={!editing}
      />
    </label>
    <p class="hint">
      Servers are created in the project the token belongs to. In the <a href="https://console.hetzner.com/" target="_blank" rel="noreferrer">Hetzner Console</a>, open the
      project, then Security → API tokens, and generate a token with the <strong>Read &amp; Write</strong> permission. Dropcatch checks that it can create servers, without
      creating one.
    </p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="pill" onclick={onclose}>Cancel</button>
    <button class="pill primary" type="submit" form="project-form" disabled={busy || !name.trim() || (!editing && !token.trim())}>{busy ? 'Checking…' : 'Save'}</button>
  {/snippet}
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
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
