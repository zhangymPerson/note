import { describe, expect, it } from "vitest";

import { buildRows, formatBytes, formatUptime, type ServerInfo } from "./info";

const full: ServerInfo = {
  version: "1.4.0",
  git_hash: "a7f74fb",
  git_dirty: false,
  build_date: "2026-10-27T12:00:00Z",
  build_label: "1.4.0 (a7f74fb, 2026-10-27T12:00:00Z)",
  go_version: "go1.26.0",
  goos: "windows",
  goarch: "amd64",
  hostname: "MY-PC",
  cpu_count: 8,
  mem_alloc_bytes: 1048576,
  mem_sys_bytes: 8388608,
  started_at: "2026-10-27T12:00:00Z",
  uptime_seconds: 3723,
  db_path: "C:\\data\\note.db",
  schema_version: 4,
  addr: "127.0.0.1:8080",
  allow_remote: false,
};

function valueOf(label: string): string {
  const row = buildRows(full).find((item) => item.label === label);
  return row?.value ?? "";
}

function rowLabels(): string[] {
  return buildRows(full).map((row) => row.label);
}

describe("buildRows", () => {
  it("把用户关心的构建信息摊成行：版本、hash、构建日期", () => {
    expect(rowLabels()).toEqual([
      "服务版本",
      "Git 构建",
      "构建日期",
      "运行平台",
      "Go 版本",
      "主机名",
      "CPU 核心数",
      "内存占用",
      "本次运行时长",
      "启动时间",
      "数据文件",
      "数据库结构版本",
      "监听地址",
      "允许远程访问",
    ]);
    expect(valueOf("服务版本")).toBe(full.build_label);
    expect(valueOf("Git 构建")).toBe("a7f74fb");
    expect(valueOf("构建日期")).toBe("2026-10-27T12:00:00Z");
    expect(valueOf("运行平台")).toBe("windows / amd64");
  });

  it("build_label 缺失时显示「未知」而不是 undefined", () => {
    const partial = { ...full, build_label: "", git_hash: "", build_date: "" };
    const rows = buildRows(partial);
    expect(rows.find((row) => row.label === "服务版本")?.value).toBe("未知");
    expect(rows.find((row) => row.label === "Git 构建")?.value).toBe("未知");
    expect(rows.find((row) => row.label === "构建日期")?.value).toBe("未知");
    expect(JSON.stringify(rows)).not.toContain("undefined");
  });

  it("工作区有未提交改动时在 hash 后面标出来", () => {
    expect(valueOf("Git 构建")).toBe("a7f74fb");
    expect(
      buildRows({ ...full, git_dirty: true }).find(
        (row) => row.label === "Git 构建",
      )?.value,
    ).toBe("a7f74fb（含未提交改动）");
  });

  it("allow_remote 摊成「是 / 否」", () => {
    expect(valueOf("允许远程访问")).toBe("否");
    expect(
      buildRows({ ...full, allow_remote: true }).find(
        (row) => row.label === "允许远程访问",
      )?.value,
    ).toBe("是");
  });

  it("数值与路径原样成文本，空路径显示「未知」", () => {
    expect(valueOf("CPU 核心数")).toBe("8");
    expect(valueOf("数据库结构版本")).toBe("4");
    expect(valueOf("监听地址")).toBe("127.0.0.1:8080");
    expect(valueOf("数据文件")).toBe("C:\\data\\note.db");
    expect(
      buildRows({ ...full, db_path: "" }).find(
        (row) => row.label === "数据文件",
      )?.value,
    ).toBe("未知");
  });

  it("内存占用同时给出已用与向系统申请的量", () => {
    expect(valueOf("内存占用")).toBe("1 MB 已用 / 8 MB 保留");
  });

  it("本次运行时长摊成小时与分", () => {
    expect(valueOf("本次运行时长")).toBe("1 小时 2 分");
  });
});

describe("formatUptime", () => {
  it("不足一分钟、整小时、跨天", () => {
    expect(formatUptime(0)).toBe("不足 1 分");
    expect(formatUptime(59)).toBe("不足 1 分");
    expect(formatUptime(60)).toBe("1 分");
    expect(formatUptime(3_723)).toBe("1 小时 2 分");
    expect(formatUptime(7_200)).toBe("2 小时");
    expect(formatUptime(90_061)).toBe("25 小时 1 分");
  });
});

describe("formatBytes", () => {
  it("按 B / KB / MB / GB 取一位小数，整数值不显示小数", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(999)).toBe("999 B");
    expect(formatBytes(1024)).toBe("1 KB");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(1048576)).toBe("1 MB");
    expect(formatBytes(8388608)).toBe("8 MB");
    expect(formatBytes(1073741824)).toBe("1 GB");
  });
});
