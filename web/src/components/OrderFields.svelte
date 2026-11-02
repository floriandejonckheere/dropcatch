<script lang="ts">
  import { api, type NamedID, type Order, type OrderInput, type Stock } from '../lib/api'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { router } from '../lib/router.svelte'

  // The automatic order of a Hetzner watch: the servers to create when its
  // server type is in stock.
  let {
    enabled = $bindable(),
    order = $bindable(),
    current,
    plan,
    architecture,
    stock,
    price,
  }: {
    enabled: boolean
    order: OrderInput
    /** The saved order, with the servers it created. */
    current?: Order
    /** The server type, the default server name. */
    plan: string
    architecture?: string
    stock: Stock[] | null
    /** Lowest monthly price, in euros. */
    price?: number
  } = $props()

  let sshKeys = $state<NamedID[] | null>(null)
  let images = $state<(NamedID & { description: string })[] | null>(null)
  let optionsError = $state('')

  const maxOrder = $derived(session.config?.limits.maxOrder ?? 20)
  const done = $derived(current && !current.active && (current.servers?.length ?? 0) >= current.quantity)

  // A project's SSH keys and the images for the server type's architecture.
  $effect(() => {
    const [project, arch] = [order.project, architecture]
    sshKeys = images = null
    optionsError = ''
    if (!enabled || !project || !arch) return
    api
      .hetznerOptions(project, arch)
      .then((o) => {
        if (project !== order.project || arch !== architecture) return
        sshKeys = o.sshKeys
        images = o.images
        // The newest Ubuntu by default, as in the Hetzner Console.
        if (!images.some((i) => i.id === order.image.id)) {
          const pick = images.find((i) => i.name.startsWith('ubuntu')) ?? images[0]
          order.image = pick ? { id: pick.id, name: pick.name } : { id: 0, name: '' }
        }
        // Keys that were removed from the project.
        order.sshKeys = order.sshKeys.filter((k) => o.sshKeys.some((x) => x.id === k.id))
        if (!order.sshKeys.length && o.sshKeys.length === 1) order.sshKeys = [o.sshKeys[0]]
      })
      .catch((e) => {
        if (project === order.project) optionsError = (e as Error).message
      })
  })

  $effect(() => {
    if (enabled && !order.project && app.projects.length) order.project = app.projects[0].id
  })

  function toggleKey(k: NamedID) {
    order.sshKeys = order.sshKeys.some((x) => x.id === k.id) ? order.sshKeys.filter((x) => x.id !== k.id) : [...order.sshKeys, { id: k.id, name: k.name }]
  }

  function chooseImage(id: number) {
    const i = images?.find((x) => x.id === id)
    if (i) order.image = { id: i.id, name: i.name }
  }

  const names = $derived.by(() => {
    const prefix = order.name.trim() || plan
    const n = Math.min(order.quantity || 1, 3)
    const list = Array.from({ length: n }, (_, i) => `${prefix}-${i + 1}`)
    return (order.quantity || 1) > 3 ? `${list.join(', ')}, …` : list.join(', ')
  })
  const euros = (n: number) => new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(n)
</script>

<fieldset>
  <legend>Order automatically</legend>
  <label class="check">
    <input type="checkbox" bind:checked={enabled} disabled={!app.projects.length && !current} />
    {done ? 'Order again when it is in stock' : 'Create servers when it is in stock'}
  </label>

  {#if current}
    {@const made = current.servers ?? []}
    {#if made.length}
      <p class="status">{done ? 'Done' : current.active ? 'Ordering' : 'Stopped'}: {made.length} of {current.quantity} created ({made.map((s) => s.name).join(', ')})</p>
    {/if}
    {#if current.error}<p class="status error">{current.active ? 'Last attempt failed' : 'Stopped'}: {current.error}</p>{/if}
  {/if}

  {#if !app.projects.length}
    <p class="hint">
      Add the Hetzner project to create them in, with a Read &amp; Write API token, under <a href={router.href('settings')}>Settings</a>.
    </p>
  {:else if enabled}
    <div class="two">
      <label class="field">
        <span>Project</span>
        <select class="input" bind:value={order.project}>
          {#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
        </select>
      </label>
      <label class="field">
        <span>Location</span>
        <select class="input" bind:value={order.location}>
          <option value="">First watched location in stock</option>
          {#each stock ?? [] as s (s.code)}<option value={s.code}>{s.name}</option>{/each}
          {#if order.location && !stock?.some((s) => s.code === order.location)}<option value={order.location}>{order.location}</option>{/if}
        </select>
      </label>
    </div>
    <div class="two">
      <label class="field">
        <span>Number of servers</span>
        <input class="input" type="number" min="1" max={maxOrder} step="1" bind:value={order.quantity} required />
      </label>
      <label class="field">
        <span>Server name</span>
        <input class="input mono" bind:value={order.name} placeholder={plan} maxlength="52" autocomplete="off" spellcheck="false" />
      </label>
    </div>
    <p class="hint">Named {names}{price ? `; ${order.quantity > 1 ? `${order.quantity} × ` : ''}from ${euros(price)}/month each, without VAT, billed by the hour` : ''}.</p>

    {#if optionsError}
      <p class="hint error">Could not load the project's images and SSH keys: {optionsError}</p>
    {:else if !architecture}
      <p class="hint">Loading the server type…</p>
    {:else if !images || !sshKeys}
      <p class="hint">Loading the project's images and SSH keys…</p>
    {:else}
      <label class="field">
        <span>Image</span>
        <select class="input" value={order.image.id} onchange={(e) => chooseImage(Number(e.currentTarget.value))}>
          {#each images as i (i.id)}<option value={i.id}>{i.description}</option>{/each}
        </select>
      </label>
      <div class="field">
        <span>SSH keys</span>
        {#if sshKeys.length}
          <div class="keys">
            {#each sshKeys as k (k.id)}
              <label class="check"><input type="checkbox" checked={order.sshKeys.some((x) => x.id === k.id)} onchange={() => toggleKey(k)} /> {k.name}</label>
            {/each}
          </div>
        {:else}
          <p class="hint error">The project has no SSH keys. Add one in the Hetzner Console (Security → SSH keys) to log in to the servers.</p>
        {/if}
      </div>
    {/if}
    <p class="hint">
      Dropcatch creates and starts the servers, one at a time, at the first check that sees stock, and notifies you, also when notifications are muted. When
      the stock runs out first, it orders the rest at the next restock. It stops when all are created, or when Hetzner refuses an order for another reason.
    </p>
  {/if}
</fieldset>

<style>
  fieldset {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 0;
    padding: 12px 14px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  legend {
    padding: 0 6px;
    font-size: 13px;
    color: var(--text-2);
  }

  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .mono {
    font-family: var(--mono);
    font-size: 14px;
  }

  .keys {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
  }

  .hint,
  .status {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-3);
  }

  .status {
    color: var(--text-2);
  }

  .error {
    color: var(--danger);
  }

  a {
    color: var(--accent-text);
  }

  @media (max-width: 600px) {
    .two {
      grid-template-columns: 1fr;
    }
  }
</style>
