import { describe, expect, it } from 'vitest'
import { matchRoute } from './router'

describe('matchRoute', () => {
  it('matches the home route', () => {
    expect(matchRoute('/')).toEqual({ name: 'home', params: {} })
  })

  it('matches the list route', () => {
    expect(matchRoute('/notes')).toEqual({ name: 'list', params: {} })
  })

  it('prefers the new route over the detail route', () => {
    expect(matchRoute('/notes/new')).toEqual({ name: 'new', params: {} })
  })

  it('parses ids for detail and action routes', () => {
    expect(matchRoute('/notes/abc')).toEqual({ name: 'detail', params: { id: 'abc' } })
    expect(matchRoute('/notes/abc/edit')).toEqual({ name: 'edit', params: { id: 'abc' } })
    expect(matchRoute('/notes/abc/forgot')).toEqual({ name: 'forgot', params: { id: 'abc' } })
  })

  it('解锁改在详情页内完成，encrypt 路由不再存在', () => {
    expect(matchRoute('/notes/abc/encrypt')).toEqual({ name: 'notFound', params: {} })
  })

  it('matches the master password and category manager routes', () => {
    expect(matchRoute('/master-password')).toEqual({ name: 'master', params: {} })
    expect(matchRoute('/categories')).toEqual({ name: 'categories', params: {} })
  })

  it('decodes id segments', () => {
    expect(matchRoute('/notes/a%20b')).toEqual({ name: 'detail', params: { id: 'a b' } })
  })

  it('returns notFound for unknown paths', () => {
    expect(matchRoute('/unknown')).toEqual({ name: 'notFound', params: {} })
  })
})
