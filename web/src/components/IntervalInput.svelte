<script lang="ts">
  import { untrack } from 'svelte'
  import { minutes } from '../lib/format'

  // A number of minutes, entered in minutes, hours or days.
  let {
    value = $bindable(),
    min,
    max,
    label = 'Interval',
    id,
  }: { value: number; min: number; max: number; label?: string; id?: string } = $props()

  type Unit = 1 | 60 | 1440
  const units: { value: Unit; label: string }[] = [
    { value: 1, label: 'minutes' },
    { value: 60, label: 'hours' },
    { value: 1440, label: 'days' },
  ]
  // Shows a value in the largest unit that fits, such as 12 hours for 720.
  const largest = (m: number): Unit => (m % 1440 === 0 ? 1440 : m % 60 === 0 ? 60 : 1)

  let unit = $state<Unit>(largest(untrack(() => value)))
  let amount = $state<number | null>(untrack(() => value) / largest(untrack(() => value)))
  let input = $state<HTMLInputElement>()
  const total = $derived(amount ? Math.round(amount * unit) : 0)

  // Follow changes from outside, such as Revert.
  $effect(() => {
    const v = value
    if (v !== untrack(() => total)) {
      unit = largest(v)
      amount = v / unit
    }
  })

  $effect(() => {
    const t = total
    if (t > 0 && t !== untrack(() => value)) value = t
  })

  // Out-of-range values block the form like any invalid field.
  $effect(() => {
    input?.setCustomValidity(total < min || total > max ? `Choose between ${minutes(min)} and ${minutes(max)}.` : '')
  })
</script>

<div class="interval" role="group" aria-label={label}>
  <input bind:this={input} {id} class="input amount" type="number" min="1" step="1" bind:value={amount} aria-label={label} required />
  <select class="input" bind:value={unit} aria-label="Unit">
    {#each units as u (u.value)}<option value={u.value}>{u.label}</option>{/each}
  </select>
</div>

<style>
  .interval {
    display: flex;
    gap: 8px;
  }

  .amount {
    width: 110px;
  }

  select.input {
    width: auto;
  }
</style>
