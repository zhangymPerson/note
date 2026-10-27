import { writable } from 'svelte/store'

export type ThemeName = 'light' | 'dark' | 'eye-care'

const STORAGE_KEY = 'note.theme'

export function resolveTheme(stored: string | null, systemPrefersDark: boolean): ThemeName {
  if (stored === 'light' || stored === 'dark' || stored === 'eye-care') {
    return stored
  }
  return systemPrefersDark ? 'dark' : 'light'
}

function detectTheme(): ThemeName {
  const stored = typeof localStorage === 'undefined' ? null : localStorage.getItem(STORAGE_KEY)
  const prefersDark =
    typeof matchMedia === 'undefined' ? false : matchMedia('(prefers-color-scheme: dark)').matches
  return resolveTheme(stored, prefersDark)
}

export const theme = writable<ThemeName>(detectTheme())

theme.subscribe((name) => {
  if (typeof document !== 'undefined') {
    document.documentElement.dataset.theme = name
  }
})

export function setTheme(name: ThemeName): void {
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(STORAGE_KEY, name)
  }
  theme.set(name)
}
