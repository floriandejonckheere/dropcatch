<script lang="ts">
  import { api } from '../lib/api'
  import { minutes } from '../lib/format'
  import { session } from '../lib/session.svelte'
  import { app } from '../lib/state.svelte'
  import { toasts } from '../lib/toasts.svelte'
  import IntervalInput from './IntervalInput.svelte'

  let interval = $state(720)
  let vpsInterval = $state(15)
  let stockInterval = $state(2)
  let timeout = $state(20)
  let notifyChanges = $state(true)
  let busy = $state(false)

  // Show the saved settings.
  function reset() {
    const s = app.settings
    interval = s.interval
    vpsInterval = s.vpsInterval
    stockInterval = s.stockInterval
    timeout = s.timeout
    notifyChanges = s.notifyChanges
  }
  $effect(() => {
    void app.settings
    reset()
  })

  const limits = $derived(session.config?.limits ?? { minInterval: 5, maxInterval: 10080, minVPSInterval: 1, maxVPSInterval: 10080, minStockInterval: 1, maxStockInterval: 60, minTimeout: 5, maxTimeout: 120 })
  const dirty = $derived(
    interval !== app.settings.interval ||
      vpsInterval !== app.settings.vpsInterval ||
      stockInterval !== app.settings.stockInterval ||
      timeout !== app.settings.timeout || notifyChanges !== app.settings.notifyChanges,
  )
  // Domains and VPS with their own interval are counted at it.
  const domainChecks = $derived(Math.round(app.domains.filter((d) => !d.paused).reduce((n, d) => n + 1440 / (d.interval || interval), 0)))
  const vpsChecks = $derived(Math.round(app.vps.filter((v) => !v.paused).reduce((n, v) => n + 1440 / (v.interval || vpsInterval), 0)))

  async function save(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    try {
      app.settings = await api.saveSettings({ interval, vpsInterval, stockInterval, timeout, notifyChanges })
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
      <span class="label">Check each domain every</span>
      <IntervalInput bind:value={interval} min={limits.minInterval} max={limits.maxInterval} label="Domain check interval" />
      <p class="hint">
        Between {minutes(limits.minInterval)} and {minutes(limits.maxInterval)}. Registrations change slowly: a few checks a day are enough.
        {#if domainChecks > 0}That is about {domainChecks.toLocaleString()} registry queries a day.{/if}
        Registries limit how often they answer; if checks fail with “rate limited”, check less often.
      </p>
    </div>

    <div class="group">
      <span class="label">Check each VPS every</span>
      <IntervalInput bind:value={vpsInterval} min={limits.minVPSInterval} max={limits.maxVPSInterval} label="VPS check interval" />
      <p class="hint">
        Between {minutes(limits.minVPSInterval)} and {minutes(limits.maxVPSInterval)}. Restocks can sell out within the hour.
        {#if vpsChecks > 0}That is about {vpsChecks.toLocaleString()} provider queries a day.{/if}
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
        Until it sells out again, so that its stock history shows when that happened; only when shorter than its usual interval. Between {minutes(limits.minStockInterval)} and
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
