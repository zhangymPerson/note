// 时间展示口径（与服务端 internal/timefmt 一致）：年月日 + 时分秒、24 小时制、
// 按系统时区换算、不带 T/Z 后缀。库里与接口传的都是 UTC 秒级文本，
// 只有展示层做这次换算。
const DISPLAY_PATTERN = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/;

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

// formatLocalStamp 把后端时间串渲染成展示格式：
//   - UTC 秒级文本 / 带偏移的 RFC3339 → 本机时区的年月日 时分秒；
//   - 已经是展示格式的串 → 原样返回（可重复调用）；
//   - 解析不了 → 原样返回，绝不把用户的时间变成空串。
export function formatLocalStamp(value: string): string {
  if (value === "" || DISPLAY_PATTERN.test(value)) return value;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}
