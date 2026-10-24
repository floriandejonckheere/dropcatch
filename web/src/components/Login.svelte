<script lang="ts">
  import { session } from '../lib/session.svelte'
  import Logo from './Logo.svelte'

  let username = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      await session.login(username, password)
    } catch (err) {
      error = (err as Error).message
      password = ''
    } finally {
      busy = false
    }
  }
</script>

<main class="login">
  <form onsubmit={submit}>
    <div class="logo"><Logo size={56} /></div>
    <h1>Dropcatch</h1>
    {#if session.config?.auth.username}
      <label class="field">
        <span>Username</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input class="input" bind:value={username} autocomplete="username" autofocus required />
      </label>
    {/if}
    <label class="field">
      <span>Password</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input class="input" type="password" bind:value={password} autocomplete="current-password" autofocus={!session.config?.auth.username} required />
    </label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button class="pill primary" type="submit" disabled={busy || !password}>{busy ? 'Signing in…' : 'Sign in'}</button>
  </form>
</main>

<style>
  .login {
    display: grid;
    place-items: center;
    min-height: 100dvh;
    padding: 16px;
  }

  form {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 16px;
    width: min(340px, 100%);
    padding: 32px 28px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 20px;
    box-shadow: var(--shadow);
  }

  .logo {
    align-self: center;
    display: flex;
  }

  h1 {
    margin: -4px 0 8px;
    text-align: center;
    font-size: 26px;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .pill {
    justify-content: center;
    height: 44px;
    margin-top: 4px;
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 14px;
  }
</style>
