import { describe, expect, it } from "vitest";

import { editorDraft } from "./draft";

describe("editorDraft", () => {
  it("保存后读回，取出即清空（只消费一次）", () => {
    editorDraft.clear();
    expect(editorDraft.current()).toBeNull();

    editorDraft.save({
      noteId: "",
      title: "草稿",
      content: "正文",
      categoryId: 2,
      encrypt: true,
    });
    expect(editorDraft.current()).toMatchObject({
      title: "草稿",
      encrypt: true,
    });

    expect(editorDraft.take("")).toMatchObject({ content: "正文" });
    expect(editorDraft.current()).toBeNull();
  });

  it("take 只交给同一条笔记的编辑器，别的笔记拿不到它", () => {
    editorDraft.clear();
    editorDraft.save({
      noteId: "n1",
      title: "甲",
      content: "甲正文",
      categoryId: null,
      encrypt: false,
    });

    // 从 n1 的编辑器跳走又进了 n2 的编辑器：n1 的正文不能被灌进 n2
    expect(editorDraft.take("n2")).toBeNull();
    expect(editorDraft.current()).toMatchObject({ noteId: "n1" });

    expect(editorDraft.take("n1")).toMatchObject({ title: "甲" });
    expect(editorDraft.current()).toBeNull();
  });

  it("清空后不留任何明文", () => {
    editorDraft.save({
      noteId: "",
      title: "a",
      content: "b",
      categoryId: null,
      encrypt: false,
    });
    editorDraft.clear();
    expect(editorDraft.current()).toBeNull();
  });
});
