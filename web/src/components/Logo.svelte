<script lang="ts">
  let { size = 28 }: { size?: number } = $props()
  // Unique per instance: the login form and the header can both be rendered.
  const id = `logo-${Math.random().toString(36).slice(2)}`
</script>

<svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
  <defs>
    <linearGradient {id} x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="var(--accent)" /><stop offset="1" stop-color="var(--accent-2)" />
    </linearGradient>
  </defs>
  <rect width="32" height="32" rx="8" fill="url(#{id})" />
  <g fill="none" stroke="#fff" stroke-width="2.4" stroke-linecap="round">
    <circle cx="16" cy="16" r="7.5" />
    <path d="M16 4.5v5M16 22.5v5M4.5 16h5M22.5 16h5" />
  </g>
  <circle cx="16" cy="16" r="2.4" fill="#fff" />
</svg>
