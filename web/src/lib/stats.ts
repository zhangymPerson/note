import { getJSON } from "./api";

export interface DayCount {
  date: string;
  count: number;
}

export interface QueryCount {
  query: string;
  count: number;
}

// KindCount 是某一类活动事件的次数，kind 与 sql/0005 的枚举一致。
export interface KindCount {
  kind: string;
  count: number;
}

export interface Stats {
  total_events: number;
  total_searches: number;
  active_days: number;
  events_by_kind: KindCount[];
  top_queries: QueryCount[];
  days: DayCount[];
}

export const HEATMAP_WEEKS = 53;

export async function loadStats(lookbackDays: number): Promise<Stats> {
  return getJSON<Stats>(`/api/stats?days=${lookbackDays}`);
}

// levelFor 把当天事件数映射为热力图 0–4 级，参考 GitHub 提交图的分级方式。
export function levelFor(count: number): 0 | 1 | 2 | 3 | 4 {
  if (count <= 0) return 0;
  if (count <= 2) return 1;
  if (count <= 5) return 2;
  if (count <= 9) return 3;
  return 4;
}

// 事件构成的展示分组：后端只回原始 kind，中文文案与合并规则留在前端。
const EVENT_GROUPS: { key: string; label: string; kinds: string[] }[] = [
  { key: "note_view", label: "查看笔记", kinds: ["note_view"] },
  { key: "note_create", label: "新建笔记", kinds: ["note_create"] },
  { key: "note_update", label: "编辑笔记", kinds: ["note_update"] },
  { key: "note_delete", label: "删除笔记", kinds: ["note_delete"] },
  { key: "search", label: "查询", kinds: ["search"] },
  {
    key: "category",
    label: "分类维护",
    kinds: ["category_create", "category_rename", "category_delete"],
  },
];

export interface EventGroup {
  key: string;
  label: string;
  count: number;
  // share 是这一组在全部事件里的占比（0–1），用于画构成条。
  share: number;
}

// groupEvents 把后端的事件类型计数折叠成固定顺序的展示分组。
// 固定分组即使为 0 也返回，让「删除 0 次」这类事实同样可见；
// 认不出的 kind（如后续新增的 note_restore）汇总为「其他」，保证总量对得上。
export function groupEvents(byKind: KindCount[]): EventGroup[] {
  const counts = new Map(byKind.map((item) => [item.kind, item.count]));
  const groups = EVENT_GROUPS.map((group) => ({
    key: group.key,
    label: group.label,
    count: group.kinds.reduce((sum, kind) => sum + (counts.get(kind) ?? 0), 0),
  }));

  const known = new Set(EVENT_GROUPS.flatMap((group) => group.kinds));
  const rest = byKind
    .filter((item) => !known.has(item.kind))
    .reduce((sum, item) => sum + item.count, 0);
  if (rest > 0) {
    groups.push({ key: "other", label: "其他", count: rest });
  }

  const total = groups.reduce((sum, group) => sum + group.count, 0);
  return groups.map((group) => ({
    ...group,
    share: total === 0 ? 0 : group.count / total,
  }));
}

export interface HeatCell {
  date: string;
  count: number;
  level: 0 | 1 | 2 | 3 | 4;
  future: boolean;
}

export interface HeatMonthLabel {
  week: number;
  text: string;
}

export interface HeatmapModel {
  weeks: HeatCell[][];
  monthLabels: HeatMonthLabel[];
  total: number;
}

function toISODate(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

// buildHeatmap 生成 HEATMAP_WEEKS 列 × 7 行（周一开头）的网格，
// 最后一天落在本周内；未来格子标记 future，仅用于占位渲染。
export function buildHeatmap(days: DayCount[], today: Date): HeatmapModel {
  const counts = new Map(days.map((d) => [d.date, d.count]));

  const last = new Date(today);
  // 归到本周一，保证网格右缘与“本周”对齐
  const weekday = (last.getDay() + 6) % 7;
  last.setDate(last.getDate() - weekday);

  const firstColumnStart = new Date(last);
  firstColumnStart.setDate(last.getDate() - (HEATMAP_WEEKS - 1) * 7);

  const weeks: HeatCell[][] = [];
  const monthLabels: HeatMonthLabel[] = [];
  let total = 0;
  let prevMonth = -1;

  for (let w = 0; w < HEATMAP_WEEKS; w += 1) {
    const column: HeatCell[] = [];
    for (let d = 0; d < 7; d += 1) {
      const cursor = new Date(firstColumnStart);
      cursor.setDate(firstColumnStart.getDate() + w * 7 + d);
      const date = toISODate(cursor);
      const future = cursor > today;
      const count = future ? 0 : (counts.get(date) ?? 0);
      if (!future) total += count;
      column.push({ date, count, level: levelFor(count), future });
    }
    const headMonth = new Date(firstColumnStart);
    headMonth.setDate(firstColumnStart.getDate() + w * 7);
    if (headMonth.getMonth() !== prevMonth) {
      monthLabels.push({ week: w, text: `${headMonth.getMonth() + 1}月` });
      prevMonth = headMonth.getMonth();
    }
    weeks.push(column);
  }

  return { weeks, monthLabels, total };
}
