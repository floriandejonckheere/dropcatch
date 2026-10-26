<script lang="ts">
  import { untrack } from 'svelte'
  import { api, type Catalog, type Plan, type Provider, type Region, type Stock, type VPS } from '../lib/api'
  import { minutes, regionLabel } from '../lib/format'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import { family, place, planLine, planSummary, providerName } from '../lib/vps'
  import Modal from './Modal.svelte'
  import OwnInterval from './OwnInterval.svelte'

  let { watch, onclose }: { watch: Partial<VPS>; onclose: () => void } = $props()

  // Edits a copy, so that cancelling leaves the watch unchanged.
  const initial = untrack(() => $state.snapshot(watch))
  const editing = !!initial.id

  let provider = $state<Provider>(initial.provider ?? 'ovh')
  let subsidiary = $state(initial.subsidiary || 'FR')
  let plan = $state(initial.plan ?? '')
  let note = $state(initial.note ?? '')
  let interval = $state(initial.interval ?? 0)
  let any = $state(!initial.datacenters?.length)
  let picked = $state<string[]>(initial.datacenters ?? [])

  let catalog = $state<Catalog | null>(null)
  let catalogError = $state('')
  let stock = $state<Stock[] | null>(null)
  let stockError = $state('')
  let loadingStock = $state(false)
  let error = $state('')
  let busy = $state(false)

  const planName = $derived(catalog?.plans.find((p) => p.code === plan)?.name ?? initial.planName ?? plan)
  const hetzner = $derived(provider === 'hetzner')
  // Hetzner needs an API token, set when starting Dropcatch.
  const noToken = $derived(hetzner && !session.config?.hetzner)
  const where = $derived(place(provider))

  // Hetzner's server types, grouped by CPU type and architecture.
  const families = $derived.by(() => {
    const groups = new Map<string, Plan[]>()
    for (const p of catalog?.plans ?? []) groups.set(family(p), [...(groups.get(family(p)) ?? []), p])
    return [...groups.entries()]
  })

  // Load the plans of the provider (and OVH subsidiary); when that fails,
  // the plan code can still be typed in. When editing, they give the
  // specifications and price of the watched plan.
  $effect(() => {
    const [prov, sub] = [provider, subsidiary]
    const current = () => prov === provider && sub === subsidiary
    catalog = null
    catalogError = ''
    if (prov === 'hetzner' && !session.config?.hetzner) return
    ;(prov === 'hetzner' ? api.hetznerPlans() : api.ovhPlans(sub))
      .then((c) => {
        if (!current()) return
        catalog = c
        if (!editing && !c.plans.some((p) => p.code === untrack(() => plan))) plan = c.plans[0]?.code ?? ''
      })
      .catch((e) => {
        if (current()) catalogError = (e as Error).message
      })
  })

  // Load the datacenters that offer the plan, with their current stock.
  let timer: ReturnType<typeof setTimeout>
  $effect(() => {
    const [prov, sub, p] = [provider, subsidiary, plan.trim()]
    const current = () => prov === provider && sub === subsidiary && p === plan.trim()
    stock = null
    stockError = ''
    clearTimeout(timer)
    loadingStock = !!p && !noToken
    if (!p || noToken) return
    // Typed plan codes are looked up once typing pauses.
    timer = setTimeout(() => {
      ;(prov === 'hetzner' ? api.hetznerStock(p) : api.ovhStock(sub, p))
        .then((r) => {
          if (current()) stock = r.datacenters
        })
        .catch((e) => {
          if (current()) stockError = (e as Error).message
        })
        .finally(() => (loadingStock = false))
    }, untrack(() => catalog) ? 0 : 500)
  })

  function choose(p: Provider) {
    if (p === provider) return
    provider = p
    plan = ''
    picked = []
  }

  const regions = $derived.by(() => {
    const groups = new Map<Region, Stock[]>()
    for (const s of stock ?? []) groups.set(s.region, [...(groups.get(s.region) ?? []), s])
    return [...groups.entries()]
  })

  function toggle(code: string) {
    picked = picked.includes(code) ? picked.filter((c) => c !== code) : [...picked, code]
  }

  function toggleRegion(dcs: Stock[]) {
    const all = dcs.every((d) => picked.includes(d.code))
    const codes = dcs.map((d) => d.code)
    picked = all ? picked.filter((c) => !codes.includes(c)) : [...new Set([...picked, ...codes])]
  }

  async function save(e: SubmitEvent) {
    e.preventDefault()
    if (!any && !picked.length) {
      error = `Choose at least one ${where}, or any ${where}.`
      return
    }
    busy = true
    error = ''
    const datacenters = any ? [] : picked
    try {
      const v = editing
        ? await api.editVPS(initial.id!, { datacenters, note, interval })
        : await api.addVPS({ provider, subsidiary: hetzner ? undefined : subsidiary, plan: plan.trim(), planName, datacenters, note, interval })
      // The first check can finish, and arrive as an event, before this
      // response: keep that newer version.
      if (editing || !app.vps.some((x) => x.id === v.id)) app.upsertVPS(v)
      toasts.show(editing ? `Saved ${v.planName}` : `Watching ${v.planName}`, { kind: 'success' })
      onclose()
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<Modal title={editing ? `Edit ${initial.planName}` : 'Watch a VPS'} {onclose} width="580px" focusField={!editing}>
  <form id="vps-form" onsubmit={save}>
    {#if editing}
      <div class="fixed">
        <p><strong>{initial.planName}</strong> <span class="muted">{planLine({ provider, plan, subsidiary })}</span></p>
        {#if catalog}
          {@const p = catalog.plans.find((x) => x.code === plan)}
          <!-- A plan OVH no longer sells is missing from the catalog. -->
          <p class="hint">{p ? planSummary(p, catalog.currency) : 'No longer in the catalog'}</p>
        {:else if !catalogError && !noToken}
          <p class="hint">Loading specifications…</p>
        {/if}
      </div>
    {:else}
      <div class="providers" role="radiogroup" aria-label="Provider">
        {#each ['ovh', 'hetzner'] as const as p (p)}
          <button type="button" role="radio" aria-checked={provider === p} class:active={provider === p} onclick={() => choose(p)}>{providerName(p)}</button>
        {/each}
      </div>
    {/if}
    {#if !editing && hetzner}
      {#if noToken}
        <p class="notice">
          Hetzner Cloud's API needs a token. Create a <strong>read-only</strong> API token in any Hetzner Cloud project (Security → API tokens) and start Dropcatch
          with <code>DROPCATCH_HETZNER_TOKEN</code> set to it.
        </p>
      {:else}
        <label class="field">
          <span>Server type</span>
          {#if catalog}
            <select class="input" bind:value={plan}>
              {#each families as [name, plans] (name)}
                <optgroup label={name}>
                  {#each plans as p (p.code)}<option value={p.code}>{p.name} · {p.cores} vCPUs · {p.memory} GB</option>{/each}
                </optgroup>
              {/each}
            </select>
          {:else}
            <input class="input mono" bind:value={plan} placeholder={catalogError ? 'cx23' : 'Loading server types…'} autocomplete="off" spellcheck="false" required />
          {/if}
        </label>
        {#if catalog}
          {@const p = catalog.plans.find((x) => x.code === plan)}
          {#if p}<p class="hint">{planSummary(p, catalog.currency)}</p>{/if}
        {:else if catalogError}
          <p class="hint">Could not load the server types ({catalogError}). Enter the name of one, such as <code>cx23</code>.</p>
        {/if}
      {/if}
    {:else if !editing}
      <div class="two">
        <label class="field">
          <span>OVHcloud shop</span>
          <select class="input" bind:value={subsidiary}>
            {#each session.config?.ovhSubsidiaries ?? [] as s (s.code)}<option value={s.code}>{s.name}</option>{/each}
          </select>
        </label>
        <label class="field">
          <span>Plan</span>
          {#if catalog}
            <select class="input" bind:value={plan}>
              {#each catalog.plans as p (p.code)}<option value={p.code}>{p.name}</option>{/each}
            </select>
          {:else}
            <input class="input mono" bind:value={plan} placeholder={catalogError ? 'vps-2027-model1' : 'Loading plans…'} autocomplete="off" spellcheck="false" required />
          {/if}
        </label>
      </div>
      {#if catalog}
        {@const p = catalog.plans.find((x) => x.code === plan)}
        {#if p}<p class="hint">{planSummary(p, catalog.currency)} <span class="mono">{p.code}</span></p>{/if}
      {:else if catalogError}
        <p class="hint">Could not load the plans ({catalogError}). Enter the plan code from the OVHcloud configurator URL, after <code>?planCode=</code>.</p>
      {/if}
      <p class="hint">The US shop sells OVHcloud US's own plans and datacenters; the others share the European, Canadian and Asia-Pacific ones.</p>
    {/if}

    {#if !noToken}
      <fieldset>
        <legend>{hetzner ? 'Locations' : 'Datacenters'}</legend>
        <label class="check"><input type="radio" bind:group={any} value={true} /> Any {where}</label>
        <label class="check"><input type="radio" bind:group={any} value={false} /> Only these:</label>

        {#if loadingStock && !stock}
          <p class="hint">Loading {where}s…</p>
        {:else if stockError}
          <p class="hint error">{stockError}</p>
        {:else if stock}
          <div class="regions" class:disabled={any}>
            {#each regions as [region, dcs] (region)}
              <div class="region">
                <label class="check region-name">
                  <input
                    type="checkbox"
                    checked={dcs.every((d) => picked.includes(d.code))}
                    indeterminate={dcs.some((d) => picked.includes(d.code)) && !dcs.every((d) => picked.includes(d.code))}
                    onchange={() => toggleRegion(dcs)}
                    disabled={any}
                  />
                  {regionLabel[region]}
                </label>
                {#each dcs as d (d.code)}
                  <label class="check dc">
                    <input type="checkbox" checked={picked.includes(d.code)} onchange={() => toggle(d.code)} disabled={any} />
                    <span>{d.name}</span>
                    <span class="stock" class:in={d.available} title={d.available ? 'In stock now' : 'Out of stock now'}></span>
                  </label>
                {/each}
              </div>
            {/each}
          </div>
          <p class="hint"><span class="stock in"></span> in stock now · <span class="stock"></span> out of stock</p>
        {/if}
      </fieldset>
    {/if}

    {#if !noToken}
      <label class="field">
        <span>Note</span>
        <input class="input" bind:value={note} placeholder="Optional" maxlength="500" />
      </label>
      <OwnInterval
        bind:value={interval}
        fallback={app.settings.vpsInterval}
        min={session.config?.limits.minVPSInterval ?? 1}
        max={session.config?.limits.maxVPSInterval ?? 10080}
        note={`While in stock, it is checked every ${minutes(app.settings.stockInterval)} if that is shorter.`}
      />
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="pill" onclick={onclose}>Cancel</button>
    <button class="pill primary" type="submit" form="vps-form" disabled={busy || noToken || (!editing && !plan.trim())}>{editing ? 'Save' : 'Watch'}</button>
  {/snippet}
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .providers {
    display: flex;
    gap: 4px;
    padding: 4px;
    border-radius: 999px;
    background: var(--bg-elev-2);
    align-self: flex-start;
  }

  .providers button {
    height: 32px;
    padding: 0 14px;
    border-radius: 999px;
    font-size: 14px;
    font-weight: 600;
    color: var(--text-2);
  }

  .providers button.active {
    background: var(--bg-elev);
    color: var(--text);
    box-shadow: 0 1px 3px rgb(0 0 0 / 0.15);
  }

  .notice {
    margin: 0;
    padding: 12px 14px;
    border-radius: var(--radius);
    background: var(--bg-elev-2);
    font-size: 14px;
    line-height: 1.5;
    color: var(--text-2);
  }

  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .fixed {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .fixed p {
    margin: 0;
  }

  .mono {
    font-family: var(--mono);
    font-size: 12px;
  }

  input.mono {
    font-size: 14px;
  }

  .hint {
    margin: -6px 0 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-3);
  }

  code {
    padding: 1px 5px;
    border-radius: 5px;
    background: var(--bg-elev-2);
    font-family: var(--mono);
    font-size: 12px;
  }

  fieldset {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0;
    padding: 12px 14px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  fieldset .hint {
    margin: 0;
  }

  legend {
    padding: 0 6px;
    font-size: 13px;
    color: var(--text-2);
  }

  .regions {
    columns: 2 220px;
    column-gap: 16px;
    padding: 4px 0 0 24px;
  }

  .regions.disabled {
    opacity: 0.45;
  }

  .region {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
    break-inside: avoid;
  }

  .region-name {
    font-weight: 650;
  }

  .dc {
    padding-left: 22px;
  }

  .stock {
    display: inline-block;
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--registered);
    opacity: 0.5;
  }

  .stock.in {
    background: var(--available);
    opacity: 1;
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 14px;
    overflow-wrap: anywhere;
  }

  @media (max-width: 600px) {
    .two {
      grid-template-columns: 1fr;
    }

    .regions {
      padding-left: 0;
    }
  }
</style>
