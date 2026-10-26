import { describe, expect, it } from 'vitest'
import { HEATMAP_WEEKS, buildHeatmap, levelFor } from './stats'

describe('levelFor', () => {
  it('maps counts to the five GitHub-like levels', () => {
    expect(levelFor(0)).toBe(0)
    expect(levelFor(1)).toBe(1)
    expect(levelFor(2)).toBe(1)
    expect(levelFor(3)).toBe(2)
    expect(levelFor(5)).toBe(2)
    expect(levelFor(6)).toBe(3)
    expect(levelFor(9)).toBe(3)
    expect(levelFor(10)).toBe(4)
  })
})

describe('buildHeatmap', () => {
  const today = new Date(2026, 9, 26) // 2026-10-26，周一

  it('produces 53 week columns of 7 cells', () => {
    const model = buildHeatmap([], today)
    expect(model.weeks).toHaveLength(HEATMAP_WEEKS)
    for (const column of model.weeks) {
      expect(column).toHaveLength(7)
    }
  })

  it('places today in the current week column', () => {
    const model = buildHeatmap([{ date: '2026-10-26', count: 4 }], today)
    const all = model.weeks.flat()
    const cell = all.find((c) => c.date === '2026-10-26')
    expect(cell?.level).toBe(2)
    expect(model.total).toBe(4)
  })

  it('marks cells after today as future', () => {
    const model = buildHeatmap([], today)
    const lastColumn = model.weeks[model.weeks.length - 1]
    const future = lastColumn.filter((cell) => cell.future)
    expect(future.length).toBeGreaterThan(0)
    for (const cell of future) {
      expect(cell.level).toBe(0)
    }
  })

  it('adds a month label when the week head crosses into a new month', () => {
    const model = buildHeatmap([], today)
    expect(model.monthLabels.length).toBeGreaterThanOrEqual(12)
    expect(model.monthLabels.length).toBeLessThanOrEqual(HEATMAP_WEEKS)
  })
})
