<script lang="ts">
  import { api } from '../lib/api'
  import { minutes } from '../lib/format'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'

  type Unit = 1 | 60 | 1440
  const units: { value: Unit; label: string }[] = [
    { value: 1, label: 'minutes' },
    { value: 60, label: 'hours' },
    { value: 1440, label: 'days' },
  ]

  let amount = $state(1)
  let unit = $state<Unit>(60)
  let stockInterval = $state(2)
  let timeout = $state(20)
  let notifyChanges = $state(true)
  let busy = $state(false)

  // Show the saved settings, in the largest unit that fits.
  function reset() {
    const s = app.settings
    const u = s.interval % 1440 === 0 ? 1440 : s.interval % 60 === 0 ? 60 : 1
    unit = u
    amount = s.interval / u
    stockInterval = s.stockInterval
    timeout = s.timeout
    notifyChanges = s.notifyChanges
  }
  $effect(() => {
    void app.settings
    reset()
  })

  const limits = $derived(session.config?.limits ?? { minInterval: 5, maxInterval: 10080, minStockInterval: 1, maxStockInterval: 60, minTimeout: 5, maxTimeout: 120 })
  const interval = $derived(Math.round(amount * unit))
  const dirty = $derived(
    interval !== app.settings.interval ||
      stockInterval !== app.settings.stockInterval ||
      timeout !== app.settings.timeout || notifyChanges !== app.settings.notifyChanges,
  )
  const checksPerDay = $derived(Math.round((1440 / interval) * app.domains.filter((d) => !d.paused).length))

  async function save(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    try {
      app.settings = await api.saveSettings({ interval, stockInterval, timeout, notifyChanges })
      toasts.show('Settings saved', { kind: 'success' })
    } catch (err) {
      toasts.error(err)
    } finally {
      busy = false
    }
  }
</script>

<section class="view">
  <h1>Settings</h1>

  <form class="card" onsubmit={save}>
    <div class="group">
      <span class="label" id="interval-label">Check every domain every</span>
      <div class="row" role="group" aria-labelledby="interval-label">
        <input class="input amount" type="number" min="1" step="1" bind:value={amount} aria-label="Interval" required />
        <select class="input" bind:value={unit} aria-label="Unit">
          {#each units as u (u.value)}<option value={u.value}>{u.label}</option>{/each}
        </select>
      </div>
      <p class="hint">
        Between {minutes(limits.minInterval)} and {minutes(limits.maxInterval)}.
        {#if checksPerDay > 0}That is about {checksPerDay.toLocaleString()} registry queries a day.{/if}
        Registries limit how often they answer; if checks fail with “rate limited”, check less often.
      </p>
    </div>

    <div class="group">
      <label class="label" for="stock-interval">Check a VPS in stock every</label>
      <div class="row">
        <input
          id="stock-interval"
          class="input amount"
          type="number"
          min={limits.minStockInterval}
          max={limits.maxStockInterval}
          step="1"
          bind:value={stockInterval}
          required
        />
        <span class="unit">minutes</span>
      </div>
      <p class="hint">
        Until it sells out again, so that its stock history shows when that happened. Between {minutes(limits.minStockInterval)} and
        {minutes(limits.maxStockInterval)}.
      </p>
    </div>

    <label class="field group">
      <span class="label">Registry timeout (seconds)</span>
      <input class="input amount" type="number" min={limits.minTimeout} max={limits.maxTimeout} bind:value={timeout} required />
    </label>

    <div class="group">
      <span class="label">Notify me when a domain</span>
      <label class="check"><input type="checkbox" checked disabled /> becomes available</label>
      <label class="check"><input type="checkbox" bind:checked={notifyChanges} /> enters its deletion period, is restored by its owner or is registered again</label>
    </div>

    <div class="buttons">
      <button class="pill" type="button" onclick={reset} disabled={!dirty || busy}>Revert</button>
      <button class="pill primary" type="submit" disabled={!dirty || busy}>Save</button>
    </div>
  </form>

  <section class="about">
    <p class="muted">
      Dropcatch {session.config?.version} · Registration data from RDAP and WHOIS ·
      <a href="https://github.com/floriandejonckheere/dropcatch" target="_blank" rel="noreferrer">Source</a>
    </p>
    {#if session.config?.auth.required}
      <button class="pill" onclick={() => session.logout()}>Sign out</button>
    {/if}
  </section>
</section>

<style>
  .view {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-width: 640px;
  }

  h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 800;
    letter-spacing: -0.01em;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 22px;
    padding: 20px;
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .label {
    font-size: 14px;
    font-weight: 600;
    color: var(--text);
  }

  .row {
    display: flex;
    gap: 8px;
  }

  .amount {
    width: 110px;
  }

  .unit {
    align-self: center;
    font-size: 14px;
    color: var(--text-2);
  }

  select.input {
    width: auto;
  }

  .hint {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-3);
  }

  .check {
    align-items: flex-start;
    line-height: 1.4;
  }

  .check input {
    flex: none;
    margin: 2px 0 0;
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  .about {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    font-size: 13px;
  }

  .about p {
    margin: 0;
  }

  .about a {
    color: var(--accent-text);
  }
</style>
