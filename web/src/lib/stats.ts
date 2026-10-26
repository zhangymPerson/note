import { getJSON } from './api'

export interface DayCount {
  date: string
  count: number
}

export interface QueryCount {
  query: string
  count: number
}

export interface Stats {
  total_searches: number
  active_days: number
  top_queries: QueryCount[]
  days: DayCount[]
}

export const HEATMAP_WEEKS = 53

export async function loadStats(lookbackDays: number): Promise<Stats> {
  return getJSON<Stats>(`/api/stats?days=${lookbackDays}`)
}

// levelFor 把查询次数映射为热力图 0–4 级，参考 GitHub 提交图的分级方式。
export function levelFor(count: number): 0 | 1 | 2 | 3 | 4 {
  if (count <= 0) return 0
  if (count <= 2) return 1
  if (count <= 5) return 2
  if (count <= 9) return 3
  return 4
}

export interface HeatCell {
  date: string
  count: number
  level: 0 | 1 | 2 | 3 | 4
  future: boolean
}

export interface HeatMonthLabel {
  week: number
  text: string
}

export interface HeatmapModel {
  weeks: HeatCell[][]
  monthLabels: HeatMonthLabel[]
  total: number
}

function toISODate(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

// buildHeatmap 生成 HEATMAP_WEEKS 列 × 7 行（周一开头）的网格，
// 最后一天落在本周内；未来格子标记 future，仅用于占位渲染。
export function buildHeatmap(days: DayCount[], today: Date): HeatmapModel {
  const counts = new Map(days.map((d) => [d.date, d.count]))

  const last = new Date(today)
  // 归到本周一，保证网格右缘与“本周”对齐
  const weekday = (last.getDay() + 6) % 7
  last.setDate(last.getDate() - weekday)

  const firstColumnStart = new Date(last)
  firstColumnStart.setDate(last.getDate() - (HEATMAP_WEEKS - 1) * 7)

  const weeks: HeatCell[][] = []
  const monthLabels: HeatMonthLabel[] = []
  let total = 0
  let prevMonth = -1

  for (let w = 0; w < HEATMAP_WEEKS; w += 1) {
    const column: HeatCell[] = []
    for (let d = 0; d < 7; d += 1) {
      const cursor = new Date(firstColumnStart)
      cursor.setDate(firstColumnStart.getDate() + w * 7 + d)
      const date = toISODate(cursor)
      const future = cursor > today
      const count = future ? 0 : (counts.get(date) ?? 0)
      if (!future) total += count
      column.push({ date, count, level: levelFor(count), future })
    }
    const headMonth = new Date(firstColumnStart)
    headMonth.setDate(firstColumnStart.getDate() + w * 7)
    if (headMonth.getMonth() !== prevMonth) {
      monthLabels.push({ week: w, text: `${headMonth.getMonth() + 1}月` })
      prevMonth = headMonth.getMonth()
    }
    weeks.push(column)
  }

  return { weeks, monthLabels, total }
}
