# 笔记合并 + 速记/合并标题本地时间 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 「全部笔记」列表可勾选多条合并成一条新笔记（正文原样拼接、原文不动），并把速记/合并自动标题的时间从 UTC `T…Z` 改为本地 `YYYY-MM-DD HH:MM:SS`。

**Architecture:** 服务端新增 `POST /api/notes/merge`（store 层 `MergeNotes` 只读原行 + 复用 `CreateNote` 落库，落「未分类」）；前端 `NoteListView.svelte` 加勾选模式与确认条；自动标题统一走 `internal/store` 的本地时间格式（`timeLayoutLocal`），前后端共用。

**Tech Stack:** Go（net/http + ServeMux）、SQLite（modernc.org/sqlite）、Svelte 5 + TypeScript、Vitest。

**Spec:** `docs/superpowers/specs/2026-11-16-note-merge-design.md`

## Global Constraints

- 状态变更类 API（POST）必须过 CSRF 双提交；含正文的响应 `Cache-Control: no-store`。
- 标题 ≤200 码点、正文 ≤2000 码点，一律 `utf8.RuneCountInString` 口径；超限不截断、明确报错。
- 服务端零密码学代码：加密笔记正文服务端不可读，合并端点整体拒绝（409 `encrypted_in_selection`）。
- 数据库结构不动，无需新迁移；标题唯一索引由 `sql/0007` 提供，撞名映射沿用 `title_conflict` / `title_in_trash`。
- 自动标题同秒撞名顺延下一秒重试 ≤5 次，永不覆盖已有笔记。
- 存库时间戳列（`created_at` / `updated_at` 等）仍为 UTC 秒级文本，**只有自动标题**用本地时间（用户明确指示，需写进 AGENTS.md 例外说明）。
- 合入前必须 `just test` 与 `just check` 全部通过。

## Review Focus

1. 选中笔记含加密 → 整体拒绝 409 `encrypted_in_selection`，不静默跳过、不 500。
2. 拼接后超 2000 码点 → 400 字段级 `content`，不截断。
3. 选中笔记在回收站（竞态窗口）→ 409 `note_in_trash`，不是 404 也不是 500。
4. 自动标题同秒撞名 → 顺延下一秒重试，合并仍成功（`合并-<第 3 秒>`）。
5. 显式标题撞名 → 409 `title_conflict`，不自动改名。

---

### Task 1: 自动标题改用本地时间（store 层）

**Files:**
- Modify: `internal/store/quick.go`
- Modify: `internal/store/quick_test.go`

**Interfaces:**
- Produces: 常量 `timeLayoutLocal = "2006-01-02 15:04:05"`；`createWithAutoTitleAt(ctx context.Context, prefix, content string, categoryID *int64, now time.Time) (Note, error)`——生成 `<prefix>-<now 自身时区的 timeLayoutLocal>` 标题、同秒撞名顺延下一秒重试 ≤5 次（`ErrNoteTitleExists` / `ErrNoteTitleInTrash` 都算撞名），落库走 `CreateNote`。`createQuickNoteAt` 的签名与行为不变（`title` 为空时内部改调 `createWithAutoTitleAt(ctx, "速记", content, &catID, now)`，`catID := int64(QuickCategoryID)`）。

- [ ] **Step 1: 改写两个失败测试**

`internal/store/quick_test.go`：
- `TestCreateQuickNoteAutoTitle`：正则改 `^速记-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`。
- `TestCreateQuickNoteRetriesSameSecond`：占位标题与 `want` 的 `Format(timeLayoutUTC)` 全改 `Format(timeLayoutLocal)`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'TestCreateQuickNote'`
Expected: FAIL（标题仍是 `T…Z` / 格式不匹配）

- [ ] **Step 3: 实现**

`internal/store/quick.go`：
- 加 `const timeLayoutLocal = "2006-01-02 15:04:05"`。
- 抽 `createWithAutoTitleAt`（把现有自动标题循环搬进去，`t := prefix + "-" + now.Add(time.Duration(i)*time.Second).Format(timeLayoutLocal)`——**不要 `.UTC()` 也不要 `.Local()`**：`time.Now()` 本身就是系统时区，测试注入的 `fixed` 用自身时区保证确定性）。
- `createQuickNoteAt` 的 `title == ""` 分支改调 `createWithAutoTitleAt(ctx, "速记", content, &catID, now)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/store/quick.go internal/store/quick_test.go
git commit -m "feat: 速记自动标题改用本地时间 YYYY-MM-DD HH:MM:SS"
```

---

### Task 2: `store.MergeNotes`

**Files:**
- Create: `internal/store/merge.go`
- Modify: `internal/store/note.go`（error var 块加两条哨兵错误）
- Create: `internal/store/merge_test.go`

**Interfaces:**
- Consumes: `createWithAutoTitleAt`（Task 1）、`CreateNote`、`maxTitleRunes`/`maxContentRunes`、`noteTestStore(t)`。
- Produces: `MergeNotes(ctx context.Context, ids []string, title string) (Note, error)`；新哨兵 `ErrNoteInTrash = errors.New("note is in trash")`、`ErrNoteEncrypted = errors.New("note is encrypted")`；常量 `maxMergeNotes = 100`。
- 语义：`ids` 按顺序把明文正文 `strings.Join(contents, "")` 成一条新笔记，落「未分类」（`NoteInput{CategoryID: nil}`）；`title` trim 后为空 → 自动 `合并-<本地时间>`；`title` 非空 → 单次创建、撞名原样返回；原文一行不写。
- 错误：`len(ids) < 2` / `> maxMergeNotes` / id 空或重复 → `ErrInvalidNote`；id 不存在 → `ErrNoteNotFound`；`deleted_at` 非空 → `ErrNoteInTrash`；`is_encrypted = 1` → `ErrNoteEncrypted`；拼接后 > `maxContentRunes` → `ErrInvalidNote`。

- [ ] **Step 1: 写失败测试**

`internal/store/merge_test.go`，用例（`noteTestStore(t)` + 直接 `CreateNote` 造数据）：

```go
func TestMergeNotes(t *testing.T) // 两条：a + b 顺序拼接、category_id nil、原文仍在且未变
func TestMergeNotesDefaultTitleFormat(t *testing.T) // 自动标题匹配 ^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$
func TestMergeNotesExplicitTitle(t *testing.T) // 显式标题用上，落未分类
func TestMergeNotesAutoTitleRetriesSameSecond(t *testing.T) // 先占住 合并-<fixed 第0秒>（未分类）与 合并-<第1秒>，再合并 → want = 合并-<第2秒>
func TestMergeNotesValidation(t *testing.T) // <2 条、重复 id → ErrInvalidNote
func TestMergeNotesMissingNote(t *testing.T) // 不存在 → ErrNoteNotFound
func TestMergeNotesTrashedNote(t *testing.T) // 软删除后 → ErrNoteInTrash
func TestMergeNotesEncryptedNote(t *testing.T) // 加密 → ErrNoteEncrypted（先 SetMasterPassword 再建密文笔记）
func TestMergeNotesContentTooLong(t *testing.T) // 两条各 1100 码点 → ErrInvalidNote
```

每条都断言关键值：拼接结果、`n.CategoryID == nil`、原文 `GetNote` 后 `Content` 未变、活动事件 `kindCount(t, st, ActivityNoteCreate) == 1`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'TestMergeNotes'`
Expected: FAIL（`MergeNotes` undefined）

- [ ] **Step 3: 实现**

`internal/store/merge.go`：按 Interfaces 语义实现 `MergeNotes`（逐条 `SELECT content, is_encrypted, deleted_at FROM notes WHERE id = ?`；`sql.ErrNoRows` → `ErrNoteNotFound`；不加事务——只读原行 + 复用 `CreateNote` 单行插入）。`note.go` error 块加两条哨兵。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/store/merge.go internal/store/merge_test.go internal/store/note.go
git commit -m "feat: store.MergeNotes 按序拼接明文正文为未分类新笔记"
```

---

### Task 3: Web 端点 `POST /api/notes/merge`

**Files:**
- Modify: `internal/web/handlers_notes.go`
- Modify: `internal/web/server.go`
- Create: `internal/web/merge_api_test.go`

**Interfaces:**
- Consumes: `app.store.MergeNotes`、`store.ErrNoteInTrash` / `store.ErrNoteEncrypted`、测试工具 `newTestHandler` / `csrfTokenFor` / `apiRequest` / `seedStore` / `decodeNote` / `errorBody` / `notesKDFParams` / `kdfParamsJSON`。
- Produces: `handleMergeNotes(w, r)`；请求 `{"ids": [...], "title": "可选"}`；201 + 新建 Note；错误映射见下。

- [ ] **Step 1: 写失败测试**

`internal/web/merge_api_test.go`（表驱动，参考 `notes_api_test.go` 风格）：

1. 成功：建两条明文 → `POST /api/notes/merge {"ids":[id1,id2]}` → 201，`decodeNote` 的 `content == "正文1正文2"`、`category_id == nil`、标题匹配 `^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`；响应头 `Cache-Control: no-store`；再 `GET /api/notes` 确认两条原文仍在。
2. 显式标题：`{"ids":[...],"title":"合并周报"}` → 201，标题就是它。
3. 无 CSRF → 403。
4. 400 字段级：`ids` 只有 1 条 / 非法 UUID / 重复 id / `title` 201 字。
5. 404：id 不存在。
6. 409 `note_in_trash`：先软删除一条再合并。
7. 409 `encrypted_in_selection`：`seedStore` 建主密码 + 加密笔记后合并（构造见 `TestNotesAPIEncryptedNote`）。
8. 409 `title_conflict`：先建一条未分类笔记标题正好等于显式合并标题，再合并。
9. 400 `content` 字段：两条各 1100 码点。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run 'TestMergeAPI'`
Expected: FAIL（404 无路由）

- [ ] **Step 3: 实现**

- `handlers_notes.go`：`mergeNotesBody{IDs []string; Title string}`；`handleMergeNotes` 校验（`len(ids) < 2` → 字段 `ids`「至少选择两条笔记」；`> 100` → 字段 `ids`；逐个 `noteIDPattern.MatchString` + 去重 → 字段 `ids`；`title` > 200 码点 → 字段 `title`）后调 `MergeNotes`；新增 `writeMergeStoreError` 映射：`ErrNoteNotFound` → 404 `not_found`；`ErrNoteInTrash` → 409 `note_in_trash`；`ErrNoteEncrypted` → 409 `encrypted_in_selection`（消息提示去掉加密笔记）；`ErrNoteTitleExists`/`ErrNoteTitleInTrash` → 409 `title_conflict`/`title_in_trash`（复用 `writeConflictFields`）；`ErrInvalidNote` → 400 字段 `content`「合并后的正文超过 2000 字上限，请减少勾选条数」（形状类错误已由 handler 预校验，store 层 `ErrInvalidNote` 只剩超长）。成功 `writeJSON(w, http.StatusCreated, note)`。
- `server.go`：加 `mux.HandleFunc("POST /api/notes/merge", app.handleMergeNotes)`（放在现有 notes 路由旁）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/web/handlers_notes.go internal/web/server.go internal/web/merge_api_test.go
git commit -m "feat: POST /api/notes/merge 合并端点（CSRF、加密/回收站/超长拒绝）"
```

---

### Task 4: 前端合并交互

**Files:**
- Modify: `web/src/lib/notes.ts`
- Modify: `web/src/views/NoteListView.svelte`
- Modify: `web/src/lib/notes.test.ts`
- Modify: `web/src/views/NoteListView.test.ts`

**Interfaces:**
- Consumes: `POST /api/notes/merge`（Task 3）、`toast`、现有 `refresh`/`listCategories`/`shownAt`。
- Produces: `mergeNotes(ids: string[], title?: string): Promise<NoteMeta>`（`postJSON("/api/notes/merge", {ids, ...(title ? {title} : {})})`）；`NoteListView` 勾选模式状态。

- [ ] **Step 1: 写失败测试**

`web/src/lib/notes.test.ts`（沿用 mock fetch 记录 calls 的写法）：`mergeNotes(["a","b"], "合并-标题")` 发出 `POST /api/notes/merge`、body 含 `{"ids":["a","b"],"title":"合并-标题"}`、带 `X-CSRF-Token`；`mergeNotes(["a","b"])` 不带 `title`。

`web/src/views/NoteListView.test.ts`：mocks 加 `mergeNotes: vi.fn()`，新增 3 条：
1. 点「合并」→ 出现复选框与确认条，选 1 条时合并按钮 disabled；选 2 条后 enabled；点「合并」→ `mergeNotes` 被调，ids 顺序 = 列表顺序，title 匹配 `^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`；成功后退出勾选模式、`toast` 触发、`listNotes` 再次被调（列表刷新）。
2. 加密行（`encrypted: true`）复选框 `disabled` 且带 title 提示。
3. 标题输入框清空后提交 → `mergeNotes` 不带 `title`（服务端回退默认）。

- [ ] **Step 2: 跑测试确认失败**

Run: `npx --prefix web vitest run src/lib/notes.test.ts src/views/NoteListView.test.ts`
Expected: FAIL（`mergeNotes` 不存在）

- [ ] **Step 3: 实现**

- `web/src/lib/notes.ts`：`mergeNotes`。
- `NoteListView.svelte`：
  - 头部「新建笔记」旁加「合并」按钮（`onclick={startMerge}`，rows 为空或 loading 时禁用）。
  - 状态：`merging`、`selectedIds: string[]`、`mergeTitle`、`mergingBusy`。
  - `startMerge()`：`merging = true; selectedIds = []; mergeTitle = 默认标题`；默认标题用 `new Date()` 生成本机时区 `合并-YYYY-MM-DD HH:MM:SS`（复用 `shownAt` 的 pad 手法，含秒）。
  - 勾选模式下：每行渲染复选框（加密行 `disabled` + title「加密笔记不能合并」）；行点击切勾选、不再展开详情；隐藏原「删除」按钮。
  - 确认条（列表顶部）：`已选 N 条` + 标题输入框（`maxlength=200`，绑 `mergeTitle`）+「合并 N 条」按钮（`selectedIds.length < 2 || mergingBusy` 时 disabled）+「取消」。
  - `doMerge()`：`mergeNotes(selectedIds, mergeTitle.trim() || undefined)` → 成功 `toast('已合并为「'+note.title+'」')`、退出勾选模式、`refresh(committedQ, category, page)` + `listCategories()`；失败 `error = message(cause)`。
  - 样式沿用现有 `.button` / `.note-row`，不引组件库。

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test`
Expected: PASS（全部前端用例）

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/notes.ts web/src/lib/notes.test.ts web/src/views/NoteListView.svelte web/src/views/NoteListView.test.ts
git commit -m "feat: 全部笔记列表合并勾选模式与确认条"
```

---

### Task 5: 文档同步 + 全量验证

**Files:**
- Modify: `docs/PRD.md`
- Modify: `docs/TECH_DESIGN.md`
- Modify: `AGENTS.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: 全部前序任务的已验证事实。

- [ ] **Step 1: 更新 PRD**

`docs/PRD.md`：升 v1.20——修订记录加「笔记合并 + 自动标题本地时间」；FR 补一条（全部笔记列表合并：勾选 ≥2 → 新笔记 `合并-<本地时间>` 落未分类、正文按序拼接、原文不动、含加密/超长/回收站拒绝）；把 v1.18/v1.19 修订记录与 FR-1.15/1.16 里 `速记-<UTC 秒级>` 措辞改为 `速记-<本地时间 YYYY-MM-DD HH:MM:SS>`（24 小时制、系统时区）。

- [ ] **Step 2: 更新 TECH_DESIGN**

`docs/TECH_DESIGN.md`：端点表（447-453 附近）补 `POST /api/notes/merge` 行；`quick.go` 目录注释（155 行）与 1039 行 API 表的 `速记-<UTC 秒级>` 改为本地时间；补一句「自动标题（速记/合并）用本地时间，是时间戳规则的唯一例外」。

- [ ] **Step 3: 更新 AGENTS.md**

`AGENTS.md` 硬性约定「时间戳统一为 UTC 秒级文本」条目末尾补：唯一例外除构建时间外，自动标题（速记 `速记-…`、合并 `合并-…`）用本地时间 `YYYY-MM-DD HH:MM:SS`（24 小时制、系统时区），仅作展示标题、不参与排序。

- [ ] **Step 4: 全量验证**

Run: `just test && just check`
Expected: Go 全包 + 前端全部用例 PASS；`go vet` 无输出；svelte-check 0 errors / 0 warnings

- [ ] **Step 5: 写 CHANGELOG 并提交**

`CHANGELOG.md` `[Unreleased]` 按 Added 分类记录：合并功能的实现过程与关键决策（端点、store 语义、前端交互、错误映射）、速记/合并标题本地时间（含「时间戳规则唯一例外」说明）、验证结果（`just test` / `just check` 通过）。文档改动随本任务一起提交：

```bash
git add docs/PRD.md docs/TECH_DESIGN.md AGENTS.md CHANGELOG.md
git commit -m "docs: 笔记合并与标题本地时间同步 PRD v1.20 / TECH_DESIGN / AGENTS.md / CHANGELOG"
```
