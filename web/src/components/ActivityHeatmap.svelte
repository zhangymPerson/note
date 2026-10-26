<script lang="ts">
  import type { DayCount } from '../lib/stats'
  import { buildHeatmap } from '../lib/stats'

  let { days }: { days: DayCount[] } = $props()

  const model = $derived(buildHeatmap(days, new Date()))

  const weekdayLabels = ['一', '', '三', '', '五', '']

  function tooltip(cell: (typeof model.weeks)[number][number]): string {
    if (cell.future) return '尚未到来'
    const [y, m, d] = cell.date.split('-')
    const label = `${y}年${Number(m)}月${Number(d)}日`
    if (cell.count === 0) return `${label} · 没有查询`
    return `${label} · ${cell.count} 次查询`
  }
</script>

<div class="heatmap" role="img" aria-label="近一年每天查询次数的热力图，颜色越深表示当天查询越多">
  <div class="heat-field">
    {#each weekdayLabels as label, row}
      <span class="heat-weekday" style="grid-row: {row + 2}">{label}</span>
    {/each}

    {#each model.monthLabels as label}
      <span class="heat-month" style="grid-column: {label.week + 2}">{label.text}</span>
    {/each}

    {#each model.weeks as column, w}
      {#each column as cell, d}
        <span
          class="heat-cell"
          style="grid-row: {d + 2}; grid-column: {w + 2}"
          data-level={cell.future ? 'empty' : cell.level}
          title={tooltip(cell)}
        ></span>
      {/each}
    {/each}
  </div>

  <div class="heat-legend">
    <span class="muted-text">少</span>
    <span class="heat-cell" data-level="0"></span>
    <span class="heat-cell" data-level="1"></span>
    <span class="heat-cell" data-level="2"></span>
    <span class="heat-cell" data-level="3"></span>
    <span class="heat-cell" data-level="4"></span>
    <span class="muted-text">多</span>
  </div>
</div>
