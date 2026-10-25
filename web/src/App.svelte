<script lang="ts">
  import DialogHost from './components/DialogHost.svelte'
  import DomainsView from './components/DomainsView.svelte'
  import Icon from './components/Icon.svelte'
  import Login from './components/Login.svelte'
  import Logo from './components/Logo.svelte'
  import Menu from './components/Menu.svelte'
  import SettingsView from './components/SettingsView.svelte'
  import TargetsView from './components/TargetsView.svelte'
  import VPSView from './components/VPSView.svelte'
  import Toasts from './components/Toasts.svelte'
  import { actions } from './lib/actions'
  import { router, type View } from './lib/router.svelte'
  import { session } from './lib/session.svelte'
  import { app } from './lib/state.svelte'
  import { toasts } from './lib/toasts.svelte'

  // Relative times ("5 minutes ago") are refreshed with this.
  let now = $state(Date.now())
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), 30_000)
    return () => clearInterval(id)
  })

  void session.load()

  $effect(() => {
    if (session.config && session.authenticated) {
      app.load().catch((e) => toasts.error(e))
      app.start()
    }
  })

  const available = $derived(
    [...app.domains, ...app.vps].filter((x) => x.status === 'available' && !x.paused).length,
  )
  $effect(() => {
    document.title = available ? `(${available}) Dropcatch` : 'Dropcatch'
  })

  const tabs: { view: View; label: string; icon: string }[] = [
    { view: 'domains', label: 'Domains', icon: 'globe' },
    { view: 'vps', label: 'VPS', icon: 'server' },
    { view: 'notifications', label: 'Notifications', icon: 'bell' },
    { view: 'settings', label: 'Settings', icon: 'settings' },
  ]

  const checkingAll = $derived(app.checking.length > 1)
</script>

{#if !session.config}
  <div class="boot">
    {#if session.error}
      <p>{session.error}</p>
      <p class="muted">Retrying every 10 seconds.</p>
      <button class="pill" onclick={() => session.load()}>Retry now</button>
    {/if}
  </div>
{:else if !session.authenticated}
  <Login />
{:else}
  <div class="app">
    <header class="top">
      <a class="logo" href={router.href('domains')} aria-label="Dropcatch home">
        <Logo />
        <span>Dropcatch</span>
      </a>
      <nav aria-label="Sections">
        {#each tabs as t (t.view)}
          <a href={router.href(t.view)} class:active={router.view === t.view} aria-current={router.view === t.view ? 'page' : undefined}>
            <Icon name={t.icon} size={16} /><span class="label">{t.label}</span>
          </a>
        {/each}
      </nav>
      <button class="pill check-all" onclick={() => actions.checkAll()} disabled={(!app.domains.length && !app.vps.length) || checkingAll} title="Check all domains and VPS now">
        <span class="icon" class:spin={app.checking.length > 0}><Icon name="refresh" size={16} /></span>
        <span class="label">{app.checking.length ? `Checking ${app.checking.length}…` : 'Check all'}</span>
      </button>
    </header>

    {#if app.offline}
      <p class="offline" role="status"><Icon name="alert" size={15} />Cannot reach the server. Retrying…</p>
    {/if}

    <main>
      {#if router.view === 'vps'}
        <VPSView {now} />
      {:else if router.view === 'notifications'}
        <TargetsView {now} />
      {:else if router.view === 'settings'}
        <SettingsView />
      {:else}
        <DomainsView {now} />
      {/if}
    </main>
  </div>
{/if}

<Menu />
<DialogHost />
<Toasts />

<style>
  .boot {
    display: grid;
    place-items: center;
    align-content: center;
    gap: 12px;
    min-height: 100dvh;
    color: var(--text-2);
  }

  .boot p {
    margin: 0;
  }

  .app {
    min-height: 100dvh;
  }

  .top {
    position: sticky;
    top: 0;
    z-index: 10;
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 12px max(20px, calc((100% - 980px) / 2));
    padding-top: calc(12px + env(safe-area-inset-top));
    background: color-mix(in srgb, var(--bg) 85%, transparent);
    backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--border);
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    font-weight: 800;
    font-size: 18px;
    letter-spacing: -0.01em;
  }

  nav {
    display: flex;
    gap: 4px;
    flex: 1;
  }

  nav a {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 36px;
    padding: 0 14px;
    border-radius: 999px;
    font-size: 14px;
    font-weight: 600;
    color: var(--text-2);
  }

  nav a:hover {
    color: var(--text);
    background: var(--bg-hover);
  }

  nav a.active {
    color: var(--text);
    background: var(--bg-elev-2);
  }

  .check-all {
    flex: none;
  }

  .icon {
    display: grid;
  }

  .offline {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin: 0;
    padding: 8px 16px;
    background: color-mix(in srgb, var(--dropping) 14%, var(--bg));
    color: var(--dropping);
    font-size: 13px;
    font-weight: 600;
  }

  main {
    padding: 24px max(20px, calc((100% - 980px) / 2)) calc(96px + env(safe-area-inset-bottom));
  }

  @media (max-width: 760px) {
    .top {
      gap: 8px;
      padding-left: 12px;
      padding-right: 12px;
    }

    .logo span,
    .label {
      display: none;
    }

    nav {
      justify-content: center;
    }

    nav a {
      padding: 0 13px;
    }

    nav a.active .label {
      display: inline;
    }

    .check-all {
      width: 38px;
      padding: 0;
      justify-content: center;
    }

    main {
      padding: 16px 12px calc(96px + env(safe-area-inset-bottom));
    }
  }
</style>
