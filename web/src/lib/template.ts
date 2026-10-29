// 新建笔记的 markdown 骨架：文件在 note.yaml 同目录，服务端读写，前端只搬运。
import { getJSON, putJSON } from "./api";
import { MAX_CONTENT_CODEPOINTS } from "./notes";

export interface NoteTemplate {
  content: string;
  // 模板文件的真实位置，页面上原样显示，方便用户去文件管理器里改。
  path: string;
  source: "file" | "default";
  // 内置默认骨架：给「恢复默认」按钮回填，前端不再自己维护一份副本。
  default_content: string;
}

// 模板会原样变成笔记正文，所以上限必须与正文一致。
export const MAX_TEMPLATE_CODEPOINTS = MAX_CONTENT_CODEPOINTS;

export function getTemplate(): Promise<NoteTemplate> {
  return getJSON<NoteTemplate>("/api/template");
}

export function saveTemplate(content: string): Promise<NoteTemplate> {
  return putJSON<NoteTemplate>("/api/template", { content });
}
