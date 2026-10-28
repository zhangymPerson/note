import { describe, expect, it } from "vitest";
import { matchRoute } from "./router";

describe("matchRoute", () => {
  it("matches the home route", () => {
    expect(matchRoute("/")).toEqual({ name: "home", params: {} });
  });

  it("matches the list route", () => {
    expect(matchRoute("/notes")).toEqual({ name: "list", params: {} });
  });

  it("prefers the new route over the detail route", () => {
    expect(matchRoute("/notes/new")).toEqual({ name: "new", params: {} });
  });

  it("parses ids for detail and action routes", () => {
    expect(matchRoute("/notes/abc")).toEqual({
      name: "detail",
      params: { id: "abc" },
    });
    expect(matchRoute("/notes/abc/edit")).toEqual({
      name: "edit",
      params: { id: "abc" },
    });
    expect(matchRoute("/notes/abc/forgot")).toEqual({
      name: "forgot",
      params: { id: "abc" },
    });
  });

  it("解锁改在详情页内完成，encrypt 路由不再存在", () => {
    expect(matchRoute("/notes/abc/encrypt")).toEqual({
      name: "notFound",
      params: {},
    });
  });

  it("matches the master password route", () => {
    expect(matchRoute("/master-password")).toEqual({
      name: "master",
      params: {},
    });
  });

  // 分类管理页并入列表页后，旧地址（含旧书签里的查询串）仍落到列表页，
  // NoteListView 挂载时的 syncUrl() 会把地址纠正成 /notes。
  it("分类页已并入列表页，旧地址落到列表", () => {
    expect(matchRoute("/categories")).toEqual({ name: "list", params: {} });
    expect(matchRoute("/categories?category=3")).toEqual({
      name: "list",
      params: {},
    });
  });

  // 编辑器交棒去设置主密码时用的是 navigate('/master-password?next=...')，
  // 带查询串就匹配不上路径，用户会看到「页面不存在」。
  it("带查询串或锚点的站内跳转仍按路径匹配", () => {
    expect(matchRoute("/master-password?next=%2Fnotes%2Fnew")).toEqual({
      name: "master",
      params: {},
    });
    expect(matchRoute("/notes/abc?from=list")).toEqual({
      name: "detail",
      params: { id: "abc" },
    });
    expect(matchRoute("/notes/abc#top")).toEqual({
      name: "detail",
      params: { id: "abc" },
    });
  });

  it("matches the info route", () => {
    expect(matchRoute("/info")).toEqual({ name: "info", params: {} });
    expect(matchRoute("/info?x=1")).toEqual({ name: "info", params: {} });
    expect(matchRoute("/info/extra")).toEqual({ name: "notFound", params: {} });
  });

  it("matches the trash route and keeps it apart from note ids", () => {
    expect(matchRoute("/trash")).toEqual({ name: "trash", params: {} });
    expect(matchRoute("/trash?page=2")).toEqual({ name: "trash", params: {} });
    expect(matchRoute("/notes/trash")).toEqual({
      name: "detail",
      params: { id: "trash" },
    });
  });

  it("decodes id segments", () => {
    expect(matchRoute("/notes/a%20b")).toEqual({
      name: "detail",
      params: { id: "a b" },
    });
  });

  it("returns notFound for unknown paths", () => {
    expect(matchRoute("/unknown")).toEqual({ name: "notFound", params: {} });
  });
});
