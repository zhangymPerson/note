let csrfToken = "";

interface ApiErrorBody {
  error?: { code: string; message: string; fields?: Record<string, string> };
}

export class ApiRequestError extends Error {
  code: string;
  fields?: Record<string, string>;

  constructor(code: string, message: string, fields?: Record<string, string>) {
    super(message);
    this.code = code;
    this.fields = fields;
  }
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let code = "http_error";
    let message = `请求失败（${response.status}）`;
    let fields: Record<string, string> | undefined;
    try {
      const body = (await response.json()) as ApiErrorBody;
      if (body.error) {
        code = body.error.code;
        message = body.error.message;
        fields = body.error.fields;
      }
    } catch {
      // 响应体不是 JSON 时使用默认文案
    }
    throw new ApiRequestError(code, message, fields);
  }
  return (await response.json()) as T;
}

async function fetchCsrfToken(): Promise<string> {
  const response = await fetch("/api/csrf");
  const body = await parseResponse<{ token: string }>(response);
  csrfToken = body.token;
  return csrfToken;
}

export async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path);
  return parseResponse<T>(response);
}

async function sendJSON<T>(
  method: string,
  path: string,
  payload: unknown,
): Promise<T> {
  if (!csrfToken) {
    await fetchCsrfToken();
  }

  const send = () =>
    fetch(path, {
      method,
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
      },
      body: JSON.stringify(payload),
    });

  let response = await send();
  if (response.status === 403) {
    // 令牌可能已过期，刷新一次后重试
    await fetchCsrfToken();
    response = await send();
  }
  return parseResponse<T>(response);
}

export async function putJSON<T>(path: string, payload: unknown): Promise<T> {
  return sendJSON<T>("PUT", path, payload);
}

export async function postJSON<T>(path: string, payload: unknown): Promise<T> {
  return sendJSON<T>("POST", path, payload);
}

// StreamEvent 是服务端推过来的一个 SSE 事件（event 名 + 已解析的 data）。
export interface StreamEvent {
  event: string;
  data: string;
}

// postEventStream 发一个会回 text/event-stream 的 POST：每读到一个事件就交给
// onEvent，函数自身等流结束（或出错）才返回。
//
// 要点：
// - 仍然走同一套 CSRF 双提交（403 刷新令牌后重试一次）；
// - 服务端在「还什么都没发」前失败时仍是普通 JSON 错误，这里照旧变成 ApiRequestError；
// - 分块解码用 TextDecoder 的 stream 模式，避免把一个多字节汉字切成两半。
export async function postEventStream(
  path: string,
  payload: unknown,
  onEvent: (event: StreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  if (!csrfToken) {
    await fetchCsrfToken();
  }

  const send = () =>
    fetch(path, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
        Accept: "text/event-stream",
      },
      body: JSON.stringify(payload),
      signal,
    });

  let response = await send();
  if (response.status === 403) {
    await fetchCsrfToken();
    response = await send();
  }
  if (!response.ok) {
    await parseResponse<never>(response);
    return;
  }
  // 兼容不认 stream 的服务端（或在代理层被退化成整段 JSON 的情形）：
  // 那就按普通 JSON 响应处理，一次性交给 onEvent 里的 done。
  if (
    !(response.headers.get("Content-Type") ?? "").includes("text/event-stream")
  ) {
    const body = (await response.json()) as {
      content?: string;
      model?: string;
    };
    onEvent({ event: "done", data: JSON.stringify(body) });
    return;
  }
  if (!response.body) {
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      let boundary = buffer.indexOf("\n\n");
      while (boundary >= 0) {
        const block = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const event = parseEventBlock(block);
        if (event) onEvent(event);
        boundary = buffer.indexOf("\n\n");
      }
    }
    buffer += decoder.decode();
    const trailing = parseEventBlock(buffer);
    if (trailing) onEvent(trailing);
  } catch (err) {
    // onEvent 抛错（error 事件）时把连接也断掉，别把上游白白跑完。
    await reader.cancel().catch(() => {});
    throw err;
  } finally {
    reader.releaseLock();
  }
}

// parseEventBlock 解析一个 SSE 事件块（`event: x` / `data: {...}` 两行）。
function parseEventBlock(block: string): StreamEvent | null {
  let event = "";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    const trimmed = line.replace(/\r$/, "");
    if (trimmed.startsWith("event:")) {
      event = trimmed.slice("event:".length).trim();
    } else if (trimmed.startsWith("data:")) {
      data.push(trimmed.slice("data:".length).trim());
    }
  }
  if (!event) return null;
  return { event, data: data.join("\n") };
}

// deleteJSON 的 204 响应没有正文，不能走 parseResponse。
export async function deleteJSON(path: string): Promise<void> {
  if (!csrfToken) {
    await fetchCsrfToken();
  }

  const send = () =>
    fetch(path, { method: "DELETE", headers: { "X-CSRF-Token": csrfToken } });

  let response = await send();
  if (response.status === 403) {
    await fetchCsrfToken();
    response = await send();
  }
  if (!response.ok) {
    await parseResponse<never>(response);
  }
  // DELETE 成功时是 204 无正文
  await response.body?.cancel();
}
