import { writable } from 'svelte/store'

export type RouteName =
  | 'home'
  | 'list'
  | 'new'
  | 'detail'
  | 'edit'
  | 'encrypt'
  | 'forgot'
  | 'notFound'

export interface Route {
  name: RouteName
  params: Record<string, string>
}

interface RoutePattern {
  name: RouteName
  pattern: RegExp
  keys: string[]
}

const patterns: RoutePattern[] = [
  { name: 'home', pattern: /^\/$/, keys: [] },
  { name: 'list', pattern: /^\/notes$/, keys: [] },
  { name: 'new', pattern: /^\/notes\/new$/, keys: [] },
  { name: 'edit', pattern: /^\/notes\/([^/]+)\/edit$/, keys: ['id'] },
  { name: 'encrypt', pattern: /^\/notes\/([^/]+)\/encrypt$/, keys: ['id'] },
  { name: 'forgot', pattern: /^\/notes\/([^/]+)\/forgot$/, keys: ['id'] },
  { name: 'detail', pattern: /^\/notes\/([^/]+)$/, keys: ['id'] },
]

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

export function matchRoute(pathname: string): Route {
  for (const candidate of patterns) {
    const match = candidate.pattern.exec(pathname)
    if (!match) continue

    const params: Record<string, string> = {}
    candidate.keys.forEach((key, index) => {
      params[key] = safeDecode(match[index + 1] ?? '')
    })
    return { name: candidate.name, params }
  }

  return { name: 'notFound', params: {} }
}

const initialPath = typeof location === 'undefined' ? '/' : location.pathname

export const route = writable<Route>(matchRoute(initialPath))

export function navigate(path: string, replace = false): void {
  if (replace) {
    history.replaceState(null, '', path)
  } else {
    history.pushState(null, '', path)
  }
  route.set(matchRoute(path))
  window.scrollTo({ top: 0 })
}

function onPopState(): void {
  route.set(matchRoute(location.pathname))
  window.scrollTo({ top: 0 })
}

export function startRouter(): void {
  window.addEventListener('popstate', onPopState)
}

export function stopRouter(): void {
  window.removeEventListener('popstate', onPopState)
}
