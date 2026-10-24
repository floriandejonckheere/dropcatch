<script lang="ts">
  import { untrack } from 'svelte'
  import { api, type Catalog, type Region, type Stock, type VPS } from '../lib/api'
  import { regionLabel } from '../lib/format'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import { planSummary } from '../lib/vps'
  import Modal from './Modal.svelte'

  let { watch, onclose }: { watch: Partial<VPS>; onclose: () => void } = $props()

  // Edits a copy, so that cancelling leaves the watch unchanged.
  const initial = untrack(() => $state.snapshot(watch))
  const editing = !!initial.id

  let subsidiary = $state(initial.subsidiary ?? 'FR')
  let plan = $state(initial.plan ?? '')
  let note = $state(initial.note ?? '')
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

  // Load the plans of the subsidiary; when that fails, the plan code can
  // still be typed in.
  $effect(() => {
    if (editing) return
    const sub = subsidiary
    catalog = null
    catalogError = ''
    api
      .ovhPlans(sub)
      .then((c) => {
        if (sub !== subsidiary) return
        catalog = c
        if (!c.plans.some((p) => p.code === untrack(() => plan))) plan = c.plans[0]?.code ?? ''
      })
      .catch((e) => {
        if (sub === subsidiary) catalogError = (e as Error).message
      })
  })

  // Load the datacenters that offer the plan, with their current stock.
  let timer: ReturnType<typeof setTimeout>
  $effect(() => {
    const [sub, p] = [subsidiary, plan.trim()]
    stock = null
    stockError = ''
    clearTimeout(timer)
    loadingStock = !!p
    if (!p) return
    // Typed plan codes are looked up once typing pauses.
    timer = setTimeout(() => {
      api
        .ovhStock(sub, p)
        .then((r) => {
          if (sub === subsidiary && p === plan.trim()) stock = r.datacenters
        })
        .catch((e) => {
          if (sub === subsidiary && p === plan.trim()) stockError = (e as Error).message
        })
        .finally(() => (loadingStock = false))
    }, untrack(() => catalog) ? 0 : 500)
  })

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
      error = 'Choose at least one datacenter, or any datacenter.'
      return
    }
    busy = true
    error = ''
    const datacenters = any ? [] : picked
    try {
      const v = editing
        ? await api.editVPS(initial.id!, { datacenters, note })
        : await api.addVPS({ subsidiary, plan: plan.trim(), planName, datacenters, note })
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

<Modal title={editing ? `Edit ${initial.planName}` : 'Watch an OVHcloud VPS'} {onclose} width="580px" focusField={!editing}>
  <form id="vps-form" onsubmit={save}>
    {#if editing}
      <p class="fixed"><strong>{initial.planName}</strong> <span class="muted">{initial.plan} · {initial.subsidiary}</span></p>
    {:else}
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

    <fieldset>
      <legend>Datacenters</legend>
      <label class="check"><input type="radio" bind:group={any} value={true} /> Any datacenter</label>
      <label class="check"><input type="radio" bind:group={any} value={false} /> Only these:</label>

      {#if loadingStock && !stock}
        <p class="hint">Loading datacenters…</p>
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

    <label class="field">
      <span>Note</span>
      <input class="input" bind:value={note} placeholder="Optional" maxlength="500" />
    </label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <button class="pill" onclick={onclose}>Cancel</button>
    <button class="pill primary" type="submit" form="vps-form" disabled={busy || (!editing && !plan.trim())}>{editing ? 'Save' : 'Watch'}</button>
  {/snippet}
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }

  .fixed {
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
