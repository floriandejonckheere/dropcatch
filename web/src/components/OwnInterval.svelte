<script lang="ts">
  import { untrack } from 'svelte'
  import { minutes } from '../lib/format'
  import IntervalInput from './IntervalInput.svelte'

  // The check interval of one domain or VPS: the setting for all of them
  // (value 0), or its own.
  let {
    value = $bindable(),
    fallback,
    min,
    max,
    note = '',
  }: { value: number; fallback: number; min: number; max: number; note?: string } = $props()

  let own = $state(untrack(() => value) > 0)
  // Starts from the default, so that switching to an own interval changes
  // nothing until it is edited.
  let minutesValue = $state(untrack(() => value || fallback))

  $effect(() => {
    value = own ? minutesValue : 0
  })
</script>

<fieldset>
  <legend>Check interval</legend>
  <label class="check"><input type="radio" bind:group={own} value={false} /> Default: every {minutes(fallback)}</label>
  <div class="own">
    <label class="check"><input type="radio" bind:group={own} value={true} /> Every</label>
    {#if own}<IntervalInput bind:value={minutesValue} {min} {max} label="Own check interval" />{/if}
  </div>
  {#if note}<p class="hint">{note}</p>{/if}
</fieldset>

<style>
  fieldset {
    display: flex;
    flex-direction: column;
    gap: 8px;
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

  .own {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
    min-height: 40px;
  }

  .hint {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-3);
  }
</style>
