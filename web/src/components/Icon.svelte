<script lang="ts" module>
  // Stroke icons after Lucide (ISC); "filled" ones use fill instead of stroke.
  const icons: Record<string, { d: string; filled?: boolean }> = {
    x: { d: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>' },
    plus: { d: '<path d="M12 5v14"/><path d="M5 12h14"/>' },
    refresh: { d: '<path d="M21 12a9 9 0 0 1-15.5 6.2L3 16"/><path d="M3 21v-5h5"/><path d="M3 12a9 9 0 0 1 15.5-6.2L21 8"/><path d="M21 3v5h-5"/>' },
    more: { d: '<circle cx="12" cy="12" r="1.3" fill="currentColor"/><circle cx="19" cy="12" r="1.3" fill="currentColor"/><circle cx="5" cy="12" r="1.3" fill="currentColor"/>' },
    globe: { d: '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>' },
    server: { d: '<rect width="20" height="8" x="2" y="2" rx="2"/><rect width="20" height="8" x="2" y="14" rx="2"/><line x1="6" x2="6.01" y1="6" y2="6"/><line x1="6" x2="6.01" y1="18" y2="18"/>' },
    'bell-off': { d: '<path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/><path d="M17 17H4a1 1 0 0 1-.7-1.7C4.6 14 6 12.6 6 8c0-.8.1-1.6.4-2.3"/><path d="M8.7 3.3A6 6 0 0 1 18 8c0 3.4.8 5 1.7 6.2"/><path d="m2 2 20 20"/>' },
    bell: { d: '<path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/><path d="M3.3 15.3A1 1 0 0 0 4 17h16a1 1 0 0 0 .7-1.7C19.4 14 18 12.6 18 8A6 6 0 0 0 6 8c0 4.6-1.4 6-2.7 7.3"/>' },
    settings: { d: '<path d="M4 21v-7"/><path d="M4 10V3"/><path d="M12 21v-9"/><path d="M12 8V3"/><path d="M20 21v-5"/><path d="M20 12V3"/><path d="M2 14h4"/><path d="M10 8h4"/><path d="M18 16h4"/>' },
    pause: { d: '<rect x="6" y="4" width="4" height="16" rx="1.2"/><rect x="14" y="4" width="4" height="16" rx="1.2"/>', filled: true },
    play: { d: '<path d="M7 4.5v15a1 1 0 0 0 1.5.86l12.5-7.5a1 1 0 0 0 0-1.72L8.5 3.64A1 1 0 0 0 7 4.5Z"/>', filled: true },
    rename: { d: '<path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>' },
    trash: { d: '<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>' },
    info: { d: '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>' },
    alert: { d: '<circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/>' },
    send: { d: '<path d="M14.5 21.7a.5.5 0 0 0 .9 0L22 2.6a.5.5 0 0 0-.6-.6L2.3 8.6a.5.5 0 0 0 0 .9l8 3.2a2 2 0 0 1 1.1 1.1Z"/><path d="m21.9 2.1-10.9 10.9"/>' },
    logout: { d: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/>' },
    search: { d: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>' },
    external: { d: '<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>' },
  }
</script>

<script lang="ts">
  let { name, size = 20 }: { name: string; size?: number } = $props()
  const icon = $derived(icons[name] ?? icons.info)
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill={icon.filled ? 'currentColor' : 'none'}
  stroke={icon.filled ? 'none' : 'currentColor'}
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {@html icon.d}
</svg>
