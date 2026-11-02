<script lang="ts">
  import { api, type Project } from '../lib/api'
  import { dialogs } from '../lib/dialogs.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import Icon from './Icon.svelte'
  import ProjectForm from './ProjectForm.svelte'

  let editing = $state<Partial<Project> | null>(null)

  const users = (p: Project) => app.vps.filter((v) => v.order?.project === p.id)

  async function remove(p: Project) {
    const ok = await dialogs.confirm({
      title: `Remove ${p.name}?`,
      message: 'Dropcatch forgets its API token. Servers created in it are not affected.',
      confirm: 'Remove',
      danger: true,
    })
    if (!ok) return
    try {
      await api.removeProject(p.id)
      app.projects = app.projects.filter((x) => x.id !== p.id)
      toasts.show(`Removed ${p.name}`)
    } catch (err) {
      toasts.error(err)
    }
  }
</script>

<section class="card projects">
  <div class="head">
    <div>
      <h2>Hetzner projects</h2>
      <p class="hint">Projects that Hetzner watches can order servers in when they are in stock, each with a Read &amp; Write API token.</p>
    </div>
    <button class="pill" onclick={() => (editing = {})}><Icon name="plus" size={16} />Add project</button>
  </div>
  {#if app.projects.length}
    <ul>
      {#each app.projects as p (p.id)}
        {@const n = users(p).length}
        <li>
          <div class="name">
            <strong>{p.name}</strong>
            <span class="muted">Token <span class="mono">{p.token}</span>{n ? ` · used by ${n} ${n === 1 ? 'order' : 'orders'}` : ''}</span>
          </div>
          <button class="icon-btn" onclick={() => (editing = p)} aria-label="Edit {p.name}" title="Edit"><Icon name="rename" size={16} /></button>
          <button class="icon-btn" onclick={() => remove(p)} aria-label="Remove {p.name}" title="Remove"><Icon name="trash" size={16} /></button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

{#if editing}
  <ProjectForm project={editing} onclose={() => (editing = null)} />
{/if}

<style>
  .projects {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 20px;
  }

  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }

  .head .pill {
    flex: none;
  }

  h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }

  .hint {
    margin: 6px 0 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-3);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  li {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 8px 0;
    border-top: 1px solid var(--border);
  }

  .name {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    font-size: 14px;
  }

  .name .muted {
    font-size: 13px;
  }

  .mono {
    font-family: var(--mono);
  }

  @media (max-width: 760px) {
    .head {
      flex-direction: column;
    }
  }
</style>
