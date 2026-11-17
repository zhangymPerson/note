import { ApiRequestError, postEventStream, postJSON } from "./api";

// 「用 AI 优化格式」的客户端：正文由服务端代发到配置好的 OpenAI 兼容接口。
// 浏览器拿不到明文密钥，所以这里只认这个端点，不直接连上游。
export interface OptimizeResult {
  content: string;
  model: string;
}

// MAX_CUSTOM_PROMPT 与 internal/ai.MaxCustomPromptRunes 同口径（500 码点）：
// 前端只用它限制输入长度，真正的校验在服务端（超长回 400 字段级 custom_prompt）。
export const MAX_CUSTOM_PROMPT = 500;

export function optimizeNote(
  content: string,
  model?: string,
  customPrompt?: string,
): Promise<OptimizeResult> {
  return postJSON<OptimizeResult>(
    "/api/ai/optimize",
    optimizePayload(content, model, customPrompt),
  );
}

// optimizeNoteStream 走同一条服务的流式版本：上游每生成一段就回调一次 onDelta，
// 最后仍以完整结果结束（done 事件里的 content 已经剥掉外层的整段围栏）。
//
// 三条路都必须处理，不能只看 happy path：
// - 服务端还没开流就失败 -> 普通 JSON 错误，抛 ApiRequestError；
// - 开始流之后失败   -> error 事件，抛 ApiRequestError（code 例如 ai_truncated）；
// - 用户中途停止     -> signal 中止，抛出原样的 AbortError，调用方自行忽略。
export function optimizeNoteStream(
  content: string,
  handlers: {
    onDelta?: (text: string) => void;
    signal?: AbortSignal;
  },
  model?: string,
  customPrompt?: string,
): Promise<OptimizeResult> {
  const payload: Record<string, unknown> = {
    ...optimizePayload(content, model, customPrompt),
    stream: true,
  };
  let result: OptimizeResult | null = null;

  return postEventStream(
    "/api/ai/optimize",
    payload,
    (event) => {
      if (event.event === "delta") {
        const body = safeParse<{ text?: string }>(event.data);
        if (body?.text) handlers.onDelta?.(body.text);
        return;
      }
      if (event.event === "error") {
        const body = safeParse<{ code?: string; message?: string }>(event.data);
        throw new ApiRequestError(
          body?.code ?? "ai_upstream_failed",
          body?.message ?? "AI 优化失败，请稍后重试",
        );
      }
      if (event.event === "done") {
        const body = safeParse<OptimizeResult>(event.data);
        if (body && typeof body.content === "string") {
          result = { content: body.content, model: body.model ?? "" };
        }
      }
    },
    handlers.signal,
  ).then(() => {
    // 没收到 done 就结束（例如上游半路断开）：宁可报错，也不能把半截结果当成品。
    if (!result) {
      throw new ApiRequestError(
        "ai_bad_response",
        "AI 没有返回完整结果，请重试",
      );
    }
    return result;
  });
}

function optimizePayload(
  content: string,
  model?: string,
  customPrompt?: string,
): Record<string, string> {
  // 不给模型就让服务端用 ai.models 的第一个：两边各存一份默认值只会对不上。
  const payload: Record<string, string> = { content };
  if (model) payload.model = model;
  // 自定义提示词只对这一次调用生效：空串就不带这个字段，服务端按「没有额外要求」处理。
  const custom = customPrompt?.trim() ?? "";
  if (custom !== "") payload.custom_prompt = custom;
  return payload;
}

// safeParse 只用于「已经确定是 JSON」的事件体；解析失败不该炸掉整次调用。
function safeParse<T>(raw: string): T | null {
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

// AI 速记：让模型生成概括标题 + 整理正文。结果只预览不落库，
// 用户在界面上确认后才调 createQuickNote 保存。
export interface QuickNoteResult {
  title: string;
  content: string;
  model: string;
}

export function aiQuickNote(content: string): Promise<QuickNoteResult> {
  return postJSON<QuickNoteResult>("/api/ai/quick", { content });
}

export function defaultModel(models: string[]): string {
  return models[0] ?? "";
}

// hostOf 是给用户看的「数据会发到哪里」：解析失败就原样显示，不假装知道。
export function hostOf(baseURL: string): string {
  try {
    return new URL(baseURL).host || baseURL;
  } catch {
    return baseURL;
  }
}
