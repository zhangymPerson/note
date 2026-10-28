import { describe, expect, it } from "vitest";
import { HEATMAP_WEEKS, buildHeatmap, groupEvents, levelFor } from "./stats";

describe("groupEvents", () => {
  it("keeps every group in a fixed order even when its count is zero", () => {
    const groups = groupEvents([{ kind: "search", count: 3 }]);
    expect(groups.map((g) => g.label)).toEqual([
      "查看笔记",
      "新建笔记",
      "编辑笔记",
      "删除笔记",
      "查询",
      "分类维护",
    ]);
    expect(groups.map((g) => g.count)).toEqual([0, 0, 0, 0, 3, 0]);
  });

  it("folds the three category kinds into 分类维护", () => {
    const groups = groupEvents([
      { kind: "category_create", count: 2 },
      { kind: "category_rename", count: 1 },
      { kind: "category_delete", count: 1 },
    ]);
    const category = groups.find((g) => g.key === "category");
    expect(category?.count).toBe(4);
    expect(category?.share).toBe(1);
  });

  it("computes shares over the displayed total", () => {
    const groups = groupEvents([
      { kind: "note_create", count: 3 },
      { kind: "search", count: 1 },
    ]);
    expect(groups.find((g) => g.key === "note_create")?.share).toBe(0.75);
    expect(groups.find((g) => g.key === "search")?.share).toBe(0.25);
  });

  it("sums unknown kinds into 其他 so the total still adds up", () => {
    const groups = groupEvents([
      { kind: "note_create", count: 1 },
      { kind: "note_restore", count: 2 },
    ]);
    const other = groups.find((g) => g.key === "other");
    expect(other?.count).toBe(2);
    expect(other?.label).toBe("其他");
  });

  it("returns zero shares when there are no events at all", () => {
    const groups = groupEvents([]);
    expect(groups.every((g) => g.share === 0)).toBe(true);
  });
});

describe("levelFor", () => {
  it("maps counts to the five GitHub-like levels", () => {
    expect(levelFor(0)).toBe(0);
    expect(levelFor(1)).toBe(1);
    expect(levelFor(2)).toBe(1);
    expect(levelFor(3)).toBe(2);
    expect(levelFor(5)).toBe(2);
    expect(levelFor(6)).toBe(3);
    expect(levelFor(9)).toBe(3);
    expect(levelFor(10)).toBe(4);
  });
});

describe("buildHeatmap", () => {
  const today = new Date(2026, 9, 26); // 2026-10-26，周一

  it("produces 53 week columns of 7 cells", () => {
    const model = buildHeatmap([], today);
    expect(model.weeks).toHaveLength(HEATMAP_WEEKS);
    for (const column of model.weeks) {
      expect(column).toHaveLength(7);
    }
  });

  it("places today in the current week column", () => {
    const model = buildHeatmap([{ date: "2026-10-26", count: 4 }], today);
    const all = model.weeks.flat();
    const cell = all.find((c) => c.date === "2026-10-26");
    expect(cell?.level).toBe(2);
    expect(model.total).toBe(4);
  });

  it("marks cells after today as future", () => {
    const model = buildHeatmap([], today);
    const lastColumn = model.weeks[model.weeks.length - 1];
    const future = lastColumn.filter((cell) => cell.future);
    expect(future.length).toBeGreaterThan(0);
    for (const cell of future) {
      expect(cell.level).toBe(0);
    }
  });

  it("adds a month label when the week head crosses into a new month", () => {
    const model = buildHeatmap([], today);
    expect(model.monthLabels.length).toBeGreaterThanOrEqual(12);
    expect(model.monthLabels.length).toBeLessThanOrEqual(HEATMAP_WEEKS);
  });
});
