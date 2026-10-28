// 信息页读取与展示：GET /api/info 的字段全是可公开的构建与运行态，
// 这里只负责把数值摊成人能读的句子，不做任何加解密。

import { getJSON } from "./api";

export interface ServerInfo {
  version: string;
  git_hash: string;
  git_dirty: boolean;
  build_date: string;
  build_label: string;
  go_version: string;
  goos: string;
  goarch: string;
  hostname: string;
  cpu_count: number;
  mem_alloc_bytes: number;
  mem_sys_bytes: number;
  started_at: string;
  uptime_seconds: number;
  db_path: string;
  schema_version: number;
  addr: string;
  allow_remote: boolean;
}

export interface InfoRow {
  label: string;
  value: string;
}

export function getInfo(): Promise<ServerInfo> {
  return getJSON<ServerInfo>("/api/info");
}

const UNKNOWN = "未知";

function text(value: string | undefined | null): string {
  const trimmed = (value ?? "").trim();
  return trimmed === "" ? UNKNOWN : trimmed;
}

export function formatBytes(value: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let index = 0;
  while (size >= 1000 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  const rounded =
    index === 0 ? String(Math.round(size)) : String(Math.round(size * 10) / 10);
  return `${rounded} ${units[index]}`;
}

export function formatUptime(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0)
    return minutes > 0 ? `${hours} 小时 ${minutes} 分` : `${hours} 小时`;
  if (minutes > 0) return `${minutes} 分`;
  return "不足 1 分";
}

export function buildRows(info: ServerInfo): InfoRow[] {
  const hash = info.git_hash.trim() === "" ? UNKNOWN : info.git_hash.trim();
  return [
    { label: "服务版本", value: text(info.build_label) },
    {
      label: "Git 构建",
      value:
        info.git_dirty && hash !== UNKNOWN ? `${hash}（含未提交改动）` : hash,
    },
    { label: "构建日期", value: text(info.build_date) },
    { label: "运行平台", value: `${text(info.goos)} / ${text(info.goarch)}` },
    { label: "Go 版本", value: text(info.go_version) },
    { label: "主机名", value: text(info.hostname) },
    { label: "CPU 核心数", value: String(info.cpu_count) },
    {
      label: "内存占用",
      value: `${formatBytes(info.mem_alloc_bytes)} 已用 / ${formatBytes(info.mem_sys_bytes)} 保留`,
    },
    { label: "本次运行时长", value: formatUptime(info.uptime_seconds) },
    { label: "启动时间", value: text(info.started_at) },
    { label: "数据文件", value: text(info.db_path) },
    { label: "数据库结构版本", value: String(info.schema_version) },
    { label: "监听地址", value: text(info.addr) },
    { label: "允许远程访问", value: info.allow_remote ? "是" : "否" },
  ];
}
