import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { loadSettings, settings, type SettingsState } from './settings'

interface Routes {
  settings?: unknown
  settingsStatus?: number
  master?: unknown
  masterStatus?: number
}

let urls: string[] = []

function json(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function mockEndpoints(routes: Routes): void {
  urls = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string | Request) => {
      const url = typeof input === 'string' ? input : input.url
      urls.push(url)
      if (url === '/api/settings') return json(routes.settings ?? { name: '', name_set: false }, routes.settingsStatus ?? 200)
      if (url === '/api/master-password') {
        return json(routes.master ?? { configured: false, salt_b64: '', question: '' }, routes.masterStatus ?? 200)
      }
      return json({ error: { code: 'not_found', message: ' unexpected ' } }, 404)
    }),
  )
}

function currentState(): SettingsState {
  let value: SettingsState | undefined
  const unsubscribe = settings.subscribe((state) => {
    value = state
  })
  unsubscribe()
  return value as SettingsState
}

beforeEach(() => {
  settings.set({ status: 'loading', name: '', nameSet: false, masterConfigured: false, error: '' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('loadSettings', () => {
  it('并行读设置与主密码配置，masterConfigured 取主密码接口的 configured', async () => {
    mockEndpoints({
      settings: { name: '张三', name_set: true },
      master: { configured: true, salt_b64: 'AAAAAAAAAAAAAAAAAAAAAA==', question: '第一只宠物叫什么' },
    })

    await loadSettings()

    expect(urls.sort()).toEqual(['/api/master-password', '/api/settings'])
    expect(currentState()).toMatchObject({ status: 'ready', name: '张三', nameSet: true, masterConfigured: true })
  })

  it('主密码尚未设置时 masterConfigured 为 false', async () => {
    settings.set({ status: 'ready', name: '张三', nameSet: true, masterConfigured: true, error: '' })
    mockEndpoints({ settings: { name: '张三', name_set: true }, master: { configured: false, salt_b64: '', question: '' } })

    await loadSettings()

    expect(currentState()).toMatchObject({ status: 'ready', nameSet: true, masterConfigured: false })
  })

  // 引导页判据建立在「名字 + 主密码」两套数据上，半套数据必须整体进错误态。
  it('主密码接口失败时整体 status 为 error', async () => {
    mockEndpoints({ settings: { name: '张三', name_set: true }, masterStatus: 500 })

    await loadSettings()

    expect(currentState().status).toBe('error')
    expect(currentState().masterConfigured).toBe(false)
  })

  it('设置接口失败时整体 status 为 error', async () => {
    mockEndpoints({ settingsStatus: 500, master: { configured: true, salt_b64: 'x', question: 'q' } })

    await loadSettings()

    expect(currentState().status).toBe('error')
  })
})
