import { getJSON, putJSON } from "./api";

// AI 四项配置存在 note.yaml 里，与 note config list 读的是同一份文件。
// 密钥永远不会以明文回到浏览器：只有掩码与「已配置」标记。
export interface AIConfig {
  base_url: string;
  // 模型列表：第一个是默认模型；界面用逗号串展示，服务端存成 YAML 序列。
  models: string[];
  prompt: string;
  // 默认提示词模板与变量名由服务端下发：网页的「恢复默认」不自己存一份文案。
  default_prompt: string;
  prompt_variable: string;
  api_key_set: boolean;
  api_key_masked: string;
  path: string;
}

export interface AIConfigPayload {
  base_url: string;
  // 逗号分隔，与服务端 config.SplitModels 同一套拆分规则。
  models: string;
  prompt: string;
  // 省略表示沿用已有密钥；空串表示清除；非空表示替换。
  api_key?: string;
}

// 提示词上限与服务端 config.MaxAIPromptRunes 保持一致。
export const AI_PROMPT_MAX_RUNES = 2000;

export async function getAIConfig(): Promise<AIConfig> {
  return getJSON<AIConfig>("/api/ai-config");
}

export async function saveAIConfig(
  payload: AIConfigPayload,
): Promise<AIConfig> {
  return putJSON<AIConfig>("/api/ai-config", payload);
}

// countRunes 按码点计数：中文按字算，与服务端 utf8.RuneCountInString 同口径。
export function countRunes(text: string): number {
  return Array.from(text).length;
}

// keyHint 说明密钥输入框当前的含义：留空是「不改动」，而不是「清空」。
export function keyHint(
  state: Pick<AIConfig, "api_key_set" | "api_key_masked">,
  clearing: boolean,
): string {
  if (clearing) return "保存后将清除密钥。";
  if (!state.api_key_set) return "还没有配置密钥。";
  return `当前已配置 ${state.api_key_masked}；留空表示不改动。`;
}
