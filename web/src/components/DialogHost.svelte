<script lang="ts">
  import { dialogs } from '../lib/dialogs.svelte'
  import Modal from './Modal.svelte'

  const d = $derived(dialogs.current)
  let value = $state('')

  $effect(() => {
    if (d?.kind === 'prompt') value = d.value
  })

  function cancel() {
    if (!d) return
    if (d.kind === 'confirm') d.resolve(false)
    else d.resolve(null)
  }
</script>

{#if d?.kind === 'confirm'}
  <Modal title={d.title} onclose={cancel}>
    <p class="msg">{d.message}</p>
    {#snippet footer()}
      <button class="pill" onclick={cancel}>Cancel</button>
      <button class="pill" class:primary={!d.danger} class:danger={d.danger} onclick={() => d.resolve(true)}>{d.confirm}</button>
    {/snippet}
  </Modal>
{:else if d?.kind === 'prompt'}
  <Modal title={d.title} onclose={cancel}>
    <form id="prompt-form" onsubmit={(e) => (e.preventDefault(), d.resolve(value))}>
      <label class="field">
        <span>{d.label}</span>
        {#if d.multiline}
          <textarea class="input" rows="3" bind:value></textarea>
        {:else}
          <input class="input" bind:value autocomplete="off" />
        {/if}
      </label>
    </form>
    {#snippet footer()}
      <button class="pill" onclick={cancel}>Cancel</button>
      <button class="pill primary" type="submit" form="prompt-form">{d.confirm}</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .msg {
    margin: 0 0 8px;
    color: var(--text-2);
    line-height: 1.5;
  }
</style>
