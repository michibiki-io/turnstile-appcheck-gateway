<script lang="ts">
  import { Label } from 'flowbite-svelte';
  export let label: string;
</script>

<label class="block">
  <Label class="mb-1">{label}</Label>
  <slot />
</label>
