import { describe, expect, it } from 'vitest'

import { decideStep } from './onboarding'

describe('decideStep', () => {
  it('名字没设过就停在第一步，与主密码是否配好无关', () => {
    expect(decideStep(false, false)).toBe(1)
    // 老库可能先设过主密码、后加的名字引导
    expect(decideStep(false, true)).toBe(1)
  })

  it('名字存了但主密码没配，仍在引导且直接进第二步', () => {
    expect(decideStep(true, false)).toBe(2)
  })

  it('两步都齐时不再进引导页', () => {
    expect(decideStep(true, true)).toBe(0)
  })
})
