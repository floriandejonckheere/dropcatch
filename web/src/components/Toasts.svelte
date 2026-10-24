<script lang="ts">
  import { fly } from 'svelte/transition'
  import { toasts } from '../lib/toasts.svelte'
  import Icon from './Icon.svelte'
</script>

<div class="toasts" aria-live="polite">
  {#each toasts.items as t (t.id)}
    <div class="toast {t.kind}" transition:fly={{ y: 16, duration: 180 }}>
      <span class="msg">{t.message}</span>
      {#if t.action}
        <button
          class="action"
          onclick={() => {
            t.action!.run()
            toasts.dismiss(t.id)
          }}>{t.action.label}</button
        >
      {/if}
      <button class="close" onclick={() => toasts.dismiss(t.id)} aria-label="Dismiss"><Icon name="x" size={14} /></button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    left: 50%;
    bottom: calc(24px + env(safe-area-inset-bottom));
    transform: translateX(-50%);
    z-index: 90;
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: min(460px, calc(100vw - 32px));
    pointer-events: none;
  }

  .toast {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 10px 10px 16px;
    border-radius: 12px;
    background: var(--text);
    color: var(--bg);
    font-size: 14px;
    box-shadow: var(--shadow);
    pointer-events: auto;
  }

  .toast.error {
    background: #d9364f;
    color: #fff;
  }

  .msg {
    flex: 1;
    min-width: 0;
  }

  .action {
    font-weight: 700;
    color: var(--accent);
    padding: 4px 6px;
  }

  .error .action {
    color: #fff;
    text-decoration: underline;
  }

  .close {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    opacity: 0.6;
  }

  .close:hover {
    opacity: 1;
  }
</style>
