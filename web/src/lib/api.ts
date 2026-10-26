let csrfToken = ''

interface ApiErrorBody {
  error?: { code: string; message: string }
}

export class ApiRequestError extends Error {
  code: string

  constructor(code: string, message: string) {
    super(message)
    this.code = code
  }
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let code = 'http_error'
    let message = `请求失败（${response.status}）`
    try {
      const body = (await response.json()) as ApiErrorBody
      if (body.error) {
        code = body.error.code
        message = body.error.message
      }
    } catch {
      // 响应体不是 JSON 时使用默认文案
    }
    throw new ApiRequestError(code, message)
  }
  return (await response.json()) as T
}

async function fetchCsrfToken(): Promise<string> {
  const response = await fetch('/api/csrf')
  const body = await parseResponse<{ token: string }>(response)
  csrfToken = body.token
  return csrfToken
}

export async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path)
  return parseResponse<T>(response)
}

export async function putJSON<T>(path: string, payload: unknown): Promise<T> {
  if (!csrfToken) {
    await fetchCsrfToken()
  }

  const send = () =>
    fetch(path, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify(payload),
    })

  let response = await send()
  if (response.status === 403) {
    // 令牌可能已过期，刷新一次后重试
    await fetchCsrfToken()
    response = await send()
  }
  return parseResponse<T>(response)
}
