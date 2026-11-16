# 笔记合并 + 速记/合并标题改用本地时间（设计）

日期：2026-11-16
状态：已批准（用户已确认）

## 1. 意图与为什么

「全部笔记」列表只能逐条查看/删除，遇到「几条碎片本来该是一条」时只能手动复制粘贴拼一条新笔记。本需求在列表上加「合并」入口：勾选多条 → 合并成一条新笔记，原文不动。同时把自动生成标题（速记、合并）的时间从 UTC 秒级 `T…Z` 格式改成用户可读的本地时间 `YYYY-MM-DD HH:MM:SS`（24 小时制、系统时区）。

## 2. 目标 / 非目标 / 反目标

**目标**

- G1 全部笔记列表（`/notes`）有「合并」按钮：点击进入勾选模式，选中 ≥2 条后可合并。
- G2 合并出的新笔记标题默认 `合并-<本地时间 YYYY-MM-DD HH:MM:SS>`（可改），正文按所选顺序直接拼接（a+b+c），不做去重/格式化/截断。
- G3 原始笔记一律不动（不删除、不修改）。
- G4 速记自动标题与合并默认标题都改用本地时间、24 小时制、`YYYY-MM-DD HH:MM:SS`，不再有 `T` / `Z`。

**非目标**

- 不合并到回收站视图（TrashView 不加按钮）。
- CLI 不加合并命令（只做网页）。
- 不处理加密笔记合并（见 §4 决策 2）。
- 不提供「合并后删除原文」选项。

**反目标**（技术上可用但等于失败）

- 静默跳过加密笔记或截断超长正文——用户不知道少了什么。
- 合并成功但原文被改动。
- 标题时间与用户所在时区不一致（必须是系统时区，不是 UTC）。

## 3. 约束（沿用现有硬性约定）

- 状态变更类 API（POST）必须过 CSRF 双提交；含正文的响应 `Cache-Control: no-store`。
- 标题 ≤200 码点、正文 ≤2000 码点，一律 `utf8.RuneCountInString` 口径；超限不截断、明确报错。
- 服务端零密码学代码：加密笔记正文服务端不可读，合并端点必须拒绝含加密笔记的请求。
- 数据库结构不动（无需新迁移；标题唯一索引由 0007 提供，合并标题撞名按现有 `title_conflict` / `title_in_trash` 映射返回）。
- 自动标题撞名策略沿用速记口径：同秒撞名自动顺延下一秒重试 ≤5 次，永不覆盖已有笔记。
- 时间戳列（`created_at` / `updated_at` 等）仍为 UTC 秒级文本，**只有自动标题**改用本地时间（用户明确指示，属有意偏离，写入 AGENTS.md 例外说明）。

## 4. 方案与取舍

**A. 独立端点（采用）**：新增 `POST /api/notes/merge`，请求 `{ "ids": [...], "title": "可选" }`，响应 = 新建的笔记（201）。
不扩展 `POST /api/notes`：合并的入参形态（id 列表）与校验语义完全不同，独立端点清晰；复用 `CreateNote` 落库保证 rev/时间戳/活动事件口径一致。

**B. 合并出的笔记分类（用户已确认）**：固定「未分类」（`category_id NULL`）。理由：选中的笔记可能跨分类，未分类最中性、无需挑第一个或弹分类选择器。

**C. 加密笔记（用户已确认）**：请求里任一 id 是加密笔记 → 整体拒绝，409 `encrypted_in_selection`，消息提示去掉加密笔记。前端在勾选模式下直接禁用加密行的复选框（带 title 提示），双保险。不静默跳过。

**D. 超长正文（用户已确认）**：拼接后超 2000 码点 → 400 字段级 `content`「合并后的正文超过 2000 字上限，请减少勾选条数」。不截断。

**E. 标题可改（沿用「默认」语义）**：勾选确认条带一个标题输入框，预填 `合并-<本地时间>`，用户可改；空标题回退默认值。服务端 `title` 缺省/空串 → 自动生成。

**F. 拼接顺序**：按请求中 `ids` 的顺序直接拼接；前端按列表展示顺序提交（勾选不改变顺序，避免「勾选顺序」在翻页后不可控）。每条正文按原样 `strings.Join(contents, "")`——原笔记正文 TrimSpace 后入库，无需再处理。

## 5. 设计决定

### 5.1 时间格式与常量

- 新增常量 `timeLayoutLocal = "2006-01-02 15:04:05"`（放在 `internal/store/quick.go`，与速记标题同文件）。
- `CreateQuickNote` 自动标题改为 `速记-` + `now.Local().Format(timeLayoutLocal)`（去掉 `.UTC()`）。
- 合并默认标题同格式：`合并-` + `time.Now().Local().Format(timeLayoutLocal)`。
- 同秒撞名顺延重试逻辑不变（对 `now.Add(i*time.Second)` 做 Local 格式化）。
- 测试用 `createQuickNoteAt(…, fixed)` 注入固定时刻（`time.Date(…, time.UTC)`），`Format` 用注入时刻自身的时区，断言仍确定。

### 5.2 `internal/store.MergeNotes(ctx, ids []string, title string) (Note, error)`

- 校验：`len(ids) < 2` → `ErrInvalidNote`（消息「至少选择两条笔记」）；`len(ids) > 100` → `ErrInvalidNote`（防病态请求）；`title` 非空时 ≤200 码点；id 集合内去重（重复 id 报错）。
- 事务内逐条 `SELECT is_encrypted, deleted_at, content FROM notes WHERE id = ?`：
  - 不存在 → `ErrNoteNotFound`；在回收站 → `ErrNoteTitleInTrash`（不可用回收站里的行合并）；
  - `is_encrypted = 1` → 新错误 `ErrNoteEncrypted`（web 层映射 409 `encrypted_in_selection`）。
- 拼接 `contents`（保持 ids 顺序），超 2000 码点 → `ErrInvalidNote`（消息含上限）。
- 默认标题 `合并-<本地时间>`，撞名顺延重试 ≤5 次（复用速记同款逻辑）；显式 title 单次创建、撞名原样返回。
- 落库复用 `CreateNote`（category_id 为 nil = 未分类）；返回 `CreateNote` 的 Note；记 `note_create` 活动事件（CreateNote 自带）。
- 原文不动：合并只读原行，不写任何 UPDATE。

### 5.3 Web 端点 `POST /api/notes/merge`

- 请求 `{ "ids": ["…"], "title": "可选" }`，body 上限 128 KiB；`ids` 每项必须匹配 `noteIDPattern`，≥2 且 ≤100 项。
- 响应：201 + 新建的 `Note`（`writeJSON` 自带 `no-store`）。
- 错误映射（复用 `writeNoteStoreError` 扩展）：`ErrNoteNotFound` → 404 `not_found`；`ErrNoteTitleInTrash` / 回收站行 → 409；`ErrNoteEncrypted` → 409 `encrypted_in_selection`（消息列明）；`ErrInvalidNote` → 400 字段级；撞名 → 409 `title_conflict` / `title_in_trash`。
- 路由：`server.go` 加 `mux.HandleFunc("POST /api/notes/merge", app.handleMergeNotes)`（注意与 `GET /api/notes/{id}` 不冲突：POST + 具体路径段）。

### 5.4 前端（`NoteListView.svelte` + `lib/notes.ts`）

- `lib/notes.ts`：`mergeNotes(ids: string[], title?: string): Promise<NoteMeta>` → `POST /api/notes/merge`。
- 头部「新建笔记」旁加「合并」按钮 → `merging = true`：
  - 每行出现复选框；加密行复选框禁用（title 提示「加密笔记不能合并」）；点击行/复选框切换选中（选中模式下列表行不再展开）。
  - 确认条带（列表上方或底部）：标题输入框（预填 `合并-<本地时间>`，本地时间格式化复用 `shownAt` 的 pad 手法）+「合并 N 条」按钮（N<2 禁用）+「取消」。
  - 提交成功 → toast、退出选中模式、刷新列表与分类计数；失败 → 内联错误展示（`encrypted_in_selection` / 超长 400 消息原样显示）。
- 不触碰 TrashView / HomeView。

### 5.5 文档

- `docs/PRD.md`：升 v1.20——修订记录（合并 + 标题本地时间）；FR 补一条合并；v1.18/v1.19 修订记录与 FR-1.15/1.16 里 `速记-<UTC 秒级>` 措辞改为 `速记-<本地时间>`。
- `docs/TECH_DESIGN.md`：端点表补 `POST /api/notes/merge`；标题自动生成段把「UTC 秒级」改为「本地时间」，注明这是时间戳规则的唯一例外。
- `AGENTS.md`：硬性约定里补一句「自动标题（速记/合并）用本地时间 `YYYY-MM-DD HH:MM:SS`，是 UTC 时间戳规则的唯一例外」。
- `CHANGELOG.md` `[Unreleased]`：实现过程、关键决策、验证结果（先跑 `just test` / `just check` 通过再写）。

### 5.6 测试

**Go**

- `internal/store`：`MergeNotes`——两条拼接（顺序、未分类、标题默认本地时间格式、原文未被改动）、三条以上、显式标题、<2 条 / >100 条 / 重复 id / 含加密 / 含回收站 / 含不存在 / 拼接超 2000 拒绝、默认标题同秒撞名顺延。
- `internal/store`（quick 改动）：自动标题正则改 `^速记-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`；同秒重试测试改用新 layout。
- `internal/web`：`POST /api/notes/merge` 表驱动——CSRF 403、成功 201 + `no-store`、ids 非法 / <2 / 超 100 / 重复 / 含加密 409 / 含回收站 409 / 超长 400 / 撞名 409、title 超 200 字段级 400。
- 前端（Vitest）：`lib/notes.ts` 的 `mergeNotes` 请求形态；`NoteListView.test.ts`——点合并进选中模式、单选不出现合并按钮（N<2 禁用）、勾选两条后可提交、提交调 `mergeNotes` 且标题为预填值、成功后退出选中模式并刷新、加密行复选框禁用。

### 5.7 验收（人工冒烟）

1. 全部笔记列表 → 「合并」→ 勾选两条 → 确认 → 出现 `合并-<本地时间>` 新笔记（未分类、正文 = 两条原文顺序拼接），原文两条仍在且未改动。
2. 选中加密笔记：复选框禁用；强行改请求 → 409 `encrypted_in_selection` 文案提示。
3. 拼接超 2000 字 → 400 明确报错，不截断。
4. 首页/CLI 新建速记标题为 `速记-2026-11-16 09:46:47` 形态（本机时区），不再有 `T…Z`。

## 6. 留给实现者的

- 确认条带与复选框的样式沿用现有 `.button` / `.note-row` 风格，不引入新组件库。
- 默认标题的本地时间在浏览器端用 `new Date()` 生成本机时区；服务端用 `time.Now()` 本机时区——两端都取「系统时区」，不传时区参数。
- `MergeNotes` 的撞名顺延：与 `CreateQuickNote` 同款重试循环，可抽一个小 helper 共用。

## 7. 我替用户做的决定（已在对话中确认）

1. **合并结果落「未分类」**（跨分类时最中性）。
2. **含加密笔记整体拒绝**（409 `encrypted_in_selection`），前端同时禁用加密行复选框，不静默跳过。
3. **拼接超 2000 码点拒绝、不截断**。
4. **标题可改**：确认条带给输入框，预填 `合并-<本地时间>`，空则回退默认。
5. **拼接顺序 = 列表展示顺序**（请求 ids 顺序，前端按列表序提交）。
6. **时间格式**：速记与合并自动标题统一 `YYYY-MM-DD HH:MM:SS` 本地时间，存库时间戳列不变（仍 UTC），这是时间戳规则的有意例外。
7. **端点命名**：`POST /api/notes/merge`。
