import { writable } from 'svelte/store'
import { getJSON, putJSON } from './api'

interface SettingsResponse {
  name: string
  name_set: boolean
}

export interface SettingsState {
  status: 'loading' | 'ready' | 'error'
  name: string
  nameSet: boolean
  error: string
}

export const settings = writable<SettingsState>({
  status: 'loading',
  name: '',
  nameSet: false,
  error: '',
})

export async function loadSettings(): Promise<void> {
  try {
    const data = await getJSON<SettingsResponse>('/api/settings')
    settings.update((state) => ({
      ...state,
      status: 'ready',
      name: data.name,
      nameSet: data.name_set,
      error: '',
    }))
  } catch (error) {
    settings.update((state) => ({
      ...state,
      status: 'error',
      error: error instanceof Error ? error.message : '无法读取设置',
    }))
  }
}

export async function saveName(name: string): Promise<void> {
  const data = await putJSON<SettingsResponse>('/api/settings/name', { name })
  settings.update((state) => ({
    ...state,
    status: 'ready',
    name: data.name,
    nameSet: data.name_set,
  }))
}
