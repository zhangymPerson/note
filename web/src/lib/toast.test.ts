import { get } from 'svelte/store'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { toast, toasts } from './toast'

afterEach(() => {
  vi.useRealTimers()
})

describe('toast', () => {
  it('shows a message until it expires', () => {
    vi.useFakeTimers()
    toast('已锁定')
    expect(get(toasts)).toEqual(['已锁定'])
    vi.advanceTimersByTime(3000)
    expect(get(toasts)).toEqual([])
  })

  it('keeps the newest toast after the older one expires', () => {
    vi.useFakeTimers()
    toast('第一条')
    vi.advanceTimersByTime(1000)
    toast('第二条')
    vi.advanceTimersByTime(2000)
    expect(get(toasts)).toEqual(['第二条'])
  })
})
