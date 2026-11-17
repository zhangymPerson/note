import { describe, expect, it } from "vitest";
import { formatLocalStamp } from "./time";

describe("formatLocalStamp", () => {
  it("把 UTC 秒级文本换算成本机时区的年月日 时分秒", () => {
    const raw = "2026-10-29T14:03:05Z";
    const got = formatLocalStamp(raw);
    expect(got).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
    // 与 Date 的本地换算同源：绝对时刻不变
    const date = new Date(raw);
    const pad = (n: number) => String(n).padStart(2, "0");
    expect(got).toBe(
      `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
        `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`,
    );
  });

  it("已带时区偏移的 RFC3339 同样换算到本机时区", () => {
    const raw = "2026-10-29T22:03:05+08:00";
    expect(formatLocalStamp(raw)).toBe(
      formatLocalStamp(new Date(raw).toISOString()),
    );
  });

  it("已经是展示格式的串原样返回（可重复调用）", () => {
    expect(formatLocalStamp("2026-10-29 22:03:05")).toBe("2026-10-29 22:03:05");
  });

  it("解析不了时原样返回，不吞掉时间", () => {
    for (const raw of ["", "not-a-time", "2026-13-45 99:99:99"]) {
      expect(formatLocalStamp(raw)).toBe(raw);
    }
  });

  it("不带 T/Z 后缀、补零到秒", () => {
    const got = formatLocalStamp("2026-01-02T03:04:05Z");
    expect(got).not.toMatch(/[TZ]/);
    expect(got).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
  });
});
