<script lang="ts">
  export let label: string;
  export let value: string;
  export let wide = false;
</script>

<div class={wide ? 'sm:col-span-2' : ''}>
  <div class="text-xs font-medium uppercase text-[var(--text-secondary)]">{label}</div>
  <div class="mt-1 break-words rounded border border-[var(--border-color)] bg-[var(--surface-elevated)] px-2 py-1 text-[var(--text-primary)]">{value || '-'}</div>
</div>
