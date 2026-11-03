import { postJSON } from "./api";

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
  // 不给模型就让服务端用 ai.models 的第一个：两边各存一份默认值只会对不上。
  const payload: Record<string, string> = { content };
  if (model) payload.model = model;
  // 自定义提示词只对这一次调用生效：空串就不带这个字段，服务端按「没有额外要求」处理。
  const custom = customPrompt?.trim() ?? "";
  if (custom !== "") payload.custom_prompt = custom;
  return postJSON<OptimizeResult>("/api/ai/optimize", payload);
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
