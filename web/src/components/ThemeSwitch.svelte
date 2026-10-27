<script lang="ts">
  import { setTheme, theme, type ThemeName } from '../lib/theme'

  const options: { value: ThemeName; label: string }[] = [
    { value: 'light', label: '亮色' },
    { value: 'dark', label: '暗色' },
    { value: 'eye-care', label: '护眼' },
  ]
</script>

<div class="theme-switch" role="group" aria-label="配色主题">
  {#each options as option}
    <button
      type="button"
      class="theme-option"
      aria-pressed={$theme === option.value}
      onclick={() => setTheme(option.value)}
    >
      {option.label}
    </button>
  {/each}
</div>
