<script lang="ts">
  import type { Snippet } from 'svelte'
  import { fade, scale } from 'svelte/transition'
  import Icon from './Icon.svelte'

  interface Props {
    title: string
    onclose: () => void
    width?: string
    children: Snippet
    footer?: Snippet
    /** Take the whole viewport (editors). */
    wide?: boolean
    /** Focus the first field on open; otherwise the dialog itself. */
    focusField?: boolean
  }
  let { title, onclose, width = '460px', children, footer, wide = false, focusField = true }: Props = $props()

  let dialog: HTMLDivElement

  $effect(() => {
    const prev = document.activeElement as HTMLElement | null
    // Focus the first input, else the dialog itself.
    const first = focusField ? dialog.querySelector<HTMLElement>('input:not([type=hidden]), textarea, select') : null
    ;(first ?? dialog).focus()
    if (first instanceof HTMLInputElement && first.type === 'text') first.select()
    return () => prev?.focus?.()
  })

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onclose()
    }
    // Keep Tab within the dialog.
    if (e.key === 'Tab') {
      const focusable = [...dialog.querySelectorAll<HTMLElement>('button, input, select, textarea, a[href], [tabindex]:not([tabindex="-1"])')].filter(
        (el) => !el.hasAttribute('disabled'),
      )
      if (!focusable.length) return
      const [first, last] = [focusable[0], focusable[focusable.length - 1]]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
  }
</script>

<div class="backdrop" transition:fade={{ duration: 120 }} onpointerdown={(e) => e.target === e.currentTarget && onclose()} role="presentation">
  <div
    bind:this={dialog}
    class="modal"
    class:wide
    style:--w={width}
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
    {onkeydown}
    transition:scale={{ start: 0.96, duration: 140 }}
  >
    <header>
      <h2 class="ellipsis">{title}</h2>
      <button class="icon-btn" onclick={onclose} aria-label="Close"><Icon name="x" size={18} /></button>
    </header>
    <div class="body">{@render children()}</div>
    {#if footer}<footer>{@render footer()}</footer>{/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 88;
    display: grid;
    place-items: center;
    padding: 16px;
    background: rgba(5, 5, 12, 0.55);
    backdrop-filter: blur(3px);
  }

  .modal {
    display: flex;
    flex-direction: column;
    width: min(var(--w), 100%);
    /* As a grid item it would otherwise grow to fit content that cannot wrap. */
    min-width: 0;
    max-height: min(88dvh, 900px);
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 16px;
    box-shadow: var(--shadow);
    outline: none;
  }

  .modal.wide {
    width: min(1200px, 100%);
    height: min(88dvh, 820px);
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 14px 6px 20px;
  }

  h2 {
    margin: 0;
    font-size: 17px;
    font-weight: 700;
  }

  .body {
    padding: 8px 20px 16px;
    overflow: auto;
    flex: 1;
    min-height: 0;
  }

  footer {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 10px;
    padding: 14px 20px 18px;
    border-top: 1px solid var(--border);
  }

  @media (max-width: 600px) {
    .backdrop {
      padding: 0;
      align-items: end;
    }

    .modal {
      width: 100%;
      max-height: 92dvh;
      border-radius: 16px 16px 0 0;
      padding-bottom: env(safe-area-inset-bottom);
    }

    .modal.wide {
      height: 100dvh;
      max-height: 100dvh;
      border-radius: 0;
    }
  }
</style>
