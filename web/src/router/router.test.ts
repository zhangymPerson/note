import { describe, expect, it } from 'vitest'
import { matchRoute } from './router'

describe('matchRoute', () => {
  it('matches the list route', () => {
    expect(matchRoute('/')).toEqual({ name: 'list', params: {} })
  })

  it('prefers the new route over the detail route', () => {
    expect(matchRoute('/notes/new')).toEqual({ name: 'new', params: {} })
  })

  it('parses ids for detail and action routes', () => {
    expect(matchRoute('/notes/abc')).toEqual({ name: 'detail', params: { id: 'abc' } })
    expect(matchRoute('/notes/abc/edit')).toEqual({ name: 'edit', params: { id: 'abc' } })
    expect(matchRoute('/notes/abc/encrypt')).toEqual({ name: 'encrypt', params: { id: 'abc' } })
    expect(matchRoute('/notes/abc/forgot')).toEqual({ name: 'forgot', params: { id: 'abc' } })
  })

  it('decodes id segments', () => {
    expect(matchRoute('/notes/a%20b')).toEqual({ name: 'detail', params: { id: 'a b' } })
  })

  it('returns notFound for unknown paths', () => {
    expect(matchRoute('/unknown')).toEqual({ name: 'notFound', params: {} })
  })
})
