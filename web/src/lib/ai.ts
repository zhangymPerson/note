import { postJSON } from "./api";

// 「用 AI 优化格式」的客户端：正文由服务端代发到配置好的 OpenAI 兼容接口。
// 浏览器拿不到明文密钥，所以这里只认这个端点，不直接连上游。
export interface OptimizeResult {
  content: string;
  model: string;
}

export function optimizeNote(
  content: string,
  model?: string,
): Promise<OptimizeResult> {
  // 不给模型就让服务端用 ai.models 的第一个：两边各存一份默认值只会对不上。
  const payload = model ? { content, model } : { content };
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
