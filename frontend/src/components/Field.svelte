<script lang="ts">
  export let label: string;
</script>

<label class="floating-field">
  <slot />
  <span class="floating-label">{label}</span>
</label>
