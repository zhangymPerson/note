<script lang="ts">
  import { highlightParts } from '../lib/highlight'

  let { text, query }: { text: string; query: string } = $props()

  const parts = $derived(highlightParts(text, query))
</script>

{#each parts as part, i (i)}
  {#if part.hit}
    <mark>{part.text}</mark>
  {:else}
    {part.text}
  {/if}
{/each}
