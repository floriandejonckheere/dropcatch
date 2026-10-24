<script lang="ts">
  import type { Status } from '../lib/api'
  import { statusLabel } from '../lib/format'

  let { status, paused = false }: { status: Status; paused?: boolean } = $props()
</script>

<span class="badge {status}" class:paused>
  <span class="dot"></span>{paused ? 'Paused' : statusLabel[status]}
</span>

<style>
  .badge {
    --c: var(--text-3);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 10px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 700;
    white-space: nowrap;
    color: var(--c);
    background: color-mix(in srgb, var(--c) 14%, transparent);
  }

  .registered {
    --c: var(--registered);
  }

  .dropping {
    --c: var(--dropping);
  }

  .available {
    --c: var(--available);
  }

  .paused {
    --c: var(--text-3);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: currentColor;
  }

  .available:not(.paused) .dot {
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--c) 30%, transparent);
  }
</style>
