<script lang="ts" module>
  export interface MenuItem {
    label: string
    icon: string
    run: () => void
    danger?: boolean
    separator?: boolean
  }

  // A single context menu for the whole app.
  class MenuState {
    open = $state<{ x: number; y: number; items: MenuItem[] } | null>(null)
    show(e: MouseEvent | { clientX: number; clientY: number }, items: MenuItem[]) {
      this.open = { x: e.clientX, y: e.clientY, items }
    }
    close() {
      this.open = null
    }
  }
  export const menu = new MenuState()
</script>

<script lang="ts">
  import Icon from './Icon.svelte'

  let el = $state<HTMLDivElement>()
  let pos = $state({ x: 0, y: 0 })

  $effect(() => {
    const m = menu.open
    if (!m || !el) return
    // Keep the menu inside the viewport.
    const r = el.getBoundingClientRect()
    pos = {
      x: Math.max(8, Math.min(m.x, innerWidth - r.width - 8)),
      y: Math.max(8, m.y + r.height > innerHeight - 8 ? m.y - r.height : m.y),
    }
    el.querySelector<HTMLButtonElement>('button')?.focus()
  })

  function onkeydown(e: KeyboardEvent) {
    if (!menu.open) return
    const buttons = [...(el?.querySelectorAll<HTMLButtonElement>('button') ?? [])]
    const i = buttons.indexOf(document.activeElement as HTMLButtonElement)
    if (e.key === 'Escape') {
      e.stopPropagation()
      menu.close()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      buttons[(i + 1) % buttons.length]?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      buttons[(i - 1 + buttons.length) % buttons.length]?.focus()
    }
  }
</script>

<svelte:window
  onkeydowncapture={onkeydown}
  onpointerdown={(e) => menu.open && !el?.contains(e.target as Node) && menu.close()}
  onresize={() => menu.close()}
/>

{#if menu.open}
  <div bind:this={el} class="menu" role="menu" style:left="{pos.x}px" style:top="{pos.y}px">
    {#each menu.open.items as item (item.label)}
      {#if item.separator}<hr />{/if}
      <button
        role="menuitem"
        class:danger={item.danger}
        onclick={() => {
          menu.close()
          item.run()
        }}
      >
        <Icon name={item.icon} size={16} />
        {item.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .menu {
    position: fixed;
    z-index: 95;
    min-width: 200px;
    padding: 6px;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
  }

  button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border-radius: 8px;
    font-size: 14px;
    text-align: left;
    color: var(--text);
  }

  button :global(svg) {
    color: var(--text-2);
  }

  button:hover,
  button:focus-visible {
    background: var(--bg-hover);
    outline: none;
  }

  button.danger,
  button.danger :global(svg) {
    color: var(--danger);
  }

  hr {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 5px 4px;
  }
</style>
