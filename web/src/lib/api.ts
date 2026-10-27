let csrfToken = ''

interface ApiErrorBody {
  error?: { code: string; message: string; fields?: Record<string, string> }
}

export class ApiRequestError extends Error {
  code: string
  fields?: Record<string, string>

  constructor(code: string, message: string, fields?: Record<string, string>) {
    super(message)
    this.code = code
    this.fields = fields
  }
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let code = 'http_error'
    let message = `请求失败（${response.status}）`
    let fields: Record<string, string> | undefined
    try {
      const body = (await response.json()) as ApiErrorBody
      if (body.error) {
        code = body.error.code
        message = body.error.message
        fields = body.error.fields
      }
    } catch {
      // 响应体不是 JSON 时使用默认文案
    }
    throw new ApiRequestError(code, message, fields)
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

async function sendJSON<T>(method: string, path: string, payload: unknown): Promise<T> {
  if (!csrfToken) {
    await fetchCsrfToken()
  }

  const send = () =>
    fetch(path, {
      method,
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

export async function putJSON<T>(path: string, payload: unknown): Promise<T> {
  return sendJSON<T>('PUT', path, payload)
}

export async function postJSON<T>(path: string, payload: unknown): Promise<T> {
  return sendJSON<T>('POST', path, payload)
}

// deleteJSON 的 204 响应没有正文，不能走 parseResponse。
export async function deleteJSON(path: string): Promise<void> {
  if (!csrfToken) {
    await fetchCsrfToken()
  }

  const send = () =>
    fetch(path, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } })

  let response = await send()
  if (response.status === 403) {
    await fetchCsrfToken()
    response = await send()
  }
  if (!response.ok) {
    await parseResponse<never>(response)
  }
  // DELETE 成功时是 204 无正文
  await response.body?.cancel()
}
