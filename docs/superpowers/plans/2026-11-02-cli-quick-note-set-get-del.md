# 速记分类与 CLI `set / get / del` 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 `note` 增加一个固定分类「速记」，以及按 `标题[@分类]` 寻址的 `note set / get / del` 三个非交互命令（`del` 为软删除）。

**Architecture:** 数据层新增迁移 0007（速记分类 + 存量重名清理 + 两条唯一索引），store 增加「按 (分类,标题) 读写」的四个方法并把唯一冲突翻译成哨兵错误；`internal/cli` 负责寻址解析与输出格式化（纯函数，注入 Deps），`cmd/note` 负责装配 store/终端与退出码，网页端只多两条 409 映射并把「固定分类」改成读 `reserved` 字段。

**Tech Stack:** Go 1.x（`net/http`、`modernc.org/sqlite`、`database/sql`）、Svelte 5 + TypeScript、Vitest、`just`。

**Spec:** `docs/superpowers/specs/2026-11-02-cli-quick-note-set-get-del-design.md`

## Global Constraints

- 时间戳统一 UTC 秒级文本 `2006-01-02T15:04:05Z`，可直接按字符串排序。
- 库结构变更只能新增 `sql/NNNN_*.sql`；既有迁移禁止修改。迁移在一个事务内整体执行，失败即回滚、`user_version` 不前进。
- 标题 1–200 码点、正文 0–2000 码点、分类名 ≤30 码点；三处（store/web/cli）同值。
- 分类名「未分类」（id=1）与「速记」（id=2）是固定分类：不可改名、不可删除，统一走 `store.ErrReservedCategory`。
- 「未分类」的笔记在库里存 `category_id IS NULL`（`store.DefaultCategoryID` ↔ NULL）。
- `set`/`del` 的提示写 stderr，stdout 只给 `get` 的正文/列表。
- 退出码：0 成功、1 找不到/没删到、2 用法错误、4 命中的是加密笔记（`get`）。
- 不加新依赖；`{@html}` 相关安全不变量本次不触碰（无正文进 DOM 的新路径）。
- 每个任务收尾必须在 `docs/CHANGELOG.md` 的 `[Unreleased]` 追加条目（Keep a Changelog 分类），且只写已跑通的测试事实。

## Review Focus

1. **存量重名数据的升级**（用户真实库里有同分类同名笔记）：迁移必须改名而不是丢数据，且改名后仍然唯一、不超 200 字符。→ Task 1 的迁移测试。
2. **网页端同名不再 500**：当地一个同分类同名请求到来时，用户看到的是 409 + 可读文案（含「同名笔记在回收站里」这种更常见的情形），而不是 `internal`。→ Task 2 + Task 7 的测试。
3. **大小写变体（`TODO` / `todo`）在 set/get/del 三条路径判定一致**：`notes.title` 列本身没有声明 `COLLATE`，查询必须显式 `COLLATE NOCASE`，否则唯一约束与查询口径会分叉。→ Task 2 的测试。
4. **`@` 出现在标题里、`@` 后为空、缺 `--yes`**：解析必须给用法错误（退出码 2），不能把 `meeting@2024` 误当分类。→ Task 3 与 Task 5 的测试。
5. **stdout 干净**：`set`/`del` 一个字节都不写 stdout；`get` 的正文原样输出，只在缺结尾换行时补一个。→ Task 4 与 Task 6 的测试。

---

## Task 1: 迁移 0007 + 「速记」固定分类 + `reserved` 字段

**Files:**
- Create: `sql/0007_quick_category_and_unique_title.sql`
- Modify: `internal/store/category.go`
- Modify: `internal/store/store_test.go:28`、`internal/store/store_test.go:72`（版本号断言 6 → 7）
- Create: `internal/store/migration_test.go`
- Modify: `internal/store/category_test.go`

**Interfaces:**
- Consumes: 既有 `sql/0001`–`0006`、`store.loadMigrations`、`store.Open`、`ListCategories`、`RenameCategory`、`DeleteCategory`。
- Produces（后续任务依赖）：
  - `store.QuickCategoryID int64 = 2`、`store.QuickCategoryName = "速记"`
  - `store.IsReservedCategory(id int64) bool`（id 1 或 2）
  - `store.Category` 新字段 `Reserved bool`，JSON 名 `reserved`
  - 数据库：`categories` 有 (2, '速记')；`notes` 上有 `idx_notes_title_category` 与 `idx_notes_title_uncategorized` 两条唯一索引；`PRAGMA user_version = 7`

- [ ] **Step 1: 写失败的测试**

`internal/store/migration_test.go`（新建）——用一个只跑到 v6 的库模拟存量数据：

```go
// openLegacyStore 建一个 user_version = 6 的库（只执行 0001–0006 的 SQL），
// 供迁移测试插入「唯一索引落不下去」的存量数据。
func openLegacyStore(t *testing.T) (string, *sql.DB) // 返回 db 路径与已打开的连接
```

实现要点：`migrations.FS` + `loadMigrations()` + `item.version <= 6` 逐条 `db.Exec`，最后 `PRAGMA user_version = 6`；`t.Cleanup` 关连接。

```go
func TestMigrationAddsQuickCategoryAndUniqueTitleIndexes(t *testing.T)
```

断言（Open 之后）：

- `SELECT id, name FROM categories WHERE id = 2` → `2 / 速记`；
- `PRAGMA user_version` → `7`；
- `SELECT name FROM sqlite_master WHERE type='index' AND name IN ('idx_notes_title_category','idx_notes_title_uncategorized')` → 两条都在。

```go
func TestMigrationDedupesTitlesKeepingLiveAndNewest(t *testing.T)
```

预置（同一「未分类」，`category_id = NULL`，content 随便非空）：

| id | title | updated_at | deleted_at |
|----|-------|-----------|-----------|
| `n1` | 周报 | 2026-01-09T00:00:00Z | 2026-01-10T00:00:00Z |
| `n2` | 周报 | 2026-01-02T00:00:00Z | NULL |
| `n3` | 周报 | 2026-01-03T00:00:00Z | NULL |

期望：`n3` 仍是 `周报`（活的最新）；`n2` → `周报 (2)`；`n1` → `周报 (3)`（在回收站里排最后，序号取组内 `ROW_NUMBER`）。

```go
func TestMigrationGroupsTitlesCaseInsensitively(t *testing.T)
```

预置分类 `id=5 name='工作'`，两条笔记 `TODO`（更新于 2026-02-02）与 `todo`（2026-02-01），同一分类。期望：新的那条保持 `TODO`，旧的那条变成 `todo (2)`，且 `SELECT COUNT(*) FROM notes WHERE title = 'TODO' COLLATE NOCASE AND category_id = 5` → 2（仍在同一组，只是名字不同）。

```go
func TestMigrationFallbackSuffixWhenCandidateTaken(t *testing.T)
```

预置同一分类三条：`周报` ×2（2026-03-02 与 2026-03-01）外加一条用户手写的 `周报 (2)`（2026-03-03，不重名）。期望：落选那条不叫 `周报 (2)`（已被占用），而是 `周报 (2 · <id 前 8 位>)`；断言 `strings.HasPrefix(renamed, "周报 (2 · ")` 且 `strings.HasSuffix(renamed, ")")`。

```go
func TestMigrationTruncatesLongRenamedTitles(t *testing.T)
```

预置两条同名、标题为 200 个 `字` 的笔记。期望：落选那条 `utf8.RuneCountInString(title) <= 200`。

`internal/store/category_test.go` 追加：

```go
func TestQuickCategoryIsReserved(t *testing.T)
```

断言：`ListCategories` 里 id=2 的 `Name == "速记"` 且 `Reserved == true`；`RenameCategory(ctx, QuickCategoryID, "快记")` 与 `DeleteCategory(ctx, QuickCategoryID)` 都返回 `ErrReservedCategory`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'TestMigration|TestQuickCategory' -v`
Expected: 编译失败（`QuickCategoryID` 未定义）或断言失败（version 6 / 无速记）。

- [ ] **Step 3: 写迁移 SQL**

`sql/0007_quick_category_and_unique_title.sql`（完整内容，逐字照抄；去重语句是实现要点，测试不接受「看起来对」）：

```sql
-- v7 速记分类 + 同分类标题唯一。
-- created_at 与其它表约定一致：UTC 秒级文本；种子行沿用 0004 的常量写法。

INSERT INTO categories (id, name, created_at) VALUES (2, '速记', '2000-01-01T00:00:00Z');

-- 存量重名清理：唯一索引的前置条件。
-- 分组键 = (category_id, 标题 NOCASE)，未分类（category_id IS NULL）自成一组。
-- 每组保留一条原名，顺序：活着的优先 → updated_at 新优先 → id 大优先；
-- 其余追加 " (序号)"，若该名字在本分类里已被占用，退化成 " (序号 · id 前 8 位)"。
-- 两种后缀在组内两两不同；改名后按 200 字符截断（应用层上限，DB 不强制）。
-- 注意 category_id 的比较必须用 IS（NULL 与 NULL 相等），不能用 =。
WITH ordered AS (
    SELECT id, title, category_id,
           ROW_NUMBER() OVER (
               PARTITION BY category_id, title COLLATE NOCASE
               ORDER BY (deleted_at IS NULL) DESC, updated_at DESC, id DESC
           ) AS rn
    FROM notes
),
plan AS (
    SELECT id, title, category_id, rn, title || ' (' || rn || ')' AS candidate
    FROM ordered
    WHERE rn > 1
),
renamed AS (
    SELECT p.id,
           CASE
               WHEN EXISTS (
                   SELECT 1 FROM notes x
                   WHERE x.id <> p.id
                     AND x.category_id IS p.category_id
                     AND x.title = p.candidate COLLATE NOCASE
               )
               THEN substr(p.title, 1, 200 - length(' (' || p.rn || ' · ' || substr(p.id, 1, 8) || ')'))
                    || ' (' || p.rn || ' · ' || substr(p.id, 1, 8) || ')'
               ELSE substr(p.title, 1, 200 - length(' (' || p.rn || ')'))
                    || ' (' || p.rn || ')'
           END AS new_title
    FROM plan p
)
UPDATE notes
SET title = (SELECT new_title FROM renamed WHERE renamed.id = notes.id)
WHERE id IN (SELECT id FROM renamed);

-- 唯一索引包含回收站里的行：恢复笔记因此永远不会撞名（回收站里的笔记不可编辑，
-- 一旦能撞名用户就没有解除手段）。未分类存的是 category_id IS NULL，而 NULL 在
-- 普通唯一索引里互不相等，所以那一组必须单独用 partial index。
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_category
    ON notes(title COLLATE NOCASE, category_id) WHERE category_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_uncategorized
    ON notes(title COLLATE NOCASE) WHERE category_id IS NULL;
```

- [ ] **Step 4: 改 `internal/store/category.go`**

```go
const quickCategoryID = 2

// QuickCategoryID 是内置「速记」分类的 id；QuickCategoryName 是它的名字。
// CLI 的 note set/get/del 默认落在这个分类（cmd/note 会断言两处常量一致）。
const (
	QuickCategoryID   = quickCategoryID
	QuickCategoryName = "速记"
)

// IsReservedCategory 判断是不是固定分类（不可改名、不可删除）。
func IsReservedCategory(id int64) bool { return id == defaultCategoryID || id == quickCategoryID }
```

- `Category` 结构体加 `Reserved bool` + ``json:"reserved"``；
- `ListCategories` 扫描后 `cat.Reserved = IsReservedCategory(cat.ID)`；
- `RenameCategory` / `DeleteCategory` 的守卫改成 `if IsReservedCategory(id) { return ErrReservedCategory }`；
- 文件头注释里补一句「id=2 速记也是固定分类」。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/store/ -v`
Expected: PASS，含 4 个新迁移测试、`TestQuickCategoryIsReserved`、以及 `TestOpenCreatesDatabaseAndAppliesMigrations`（版本 7）。

- [ ] **Step 6: CHANGELOG + 提交**

`docs/CHANGELOG.md` 的 `[Unreleased] → Added` 追加：固定分类「速记」（id=2，与未分类同为不可改名的内置分类）；`Changed` 追加：迁移 0007 清理存量同分类同名笔记（活的/新的保留原名，其余追加序号）并新增两条唯一索引（含回收站行）。

```bash
git add sql/0007_quick_category_and_unique_title.sql internal/store/ docs/CHANGELOG.md
git commit -m "feat(store): 速记固定分类与同分类标题唯一索引"
```

---

## Task 2: store 层按 (分类, 标题) 读写 + 唯一冲突翻译

**Files:**
- Create: `internal/store/note_title.go`
- Create: `internal/store/note_title_test.go`
- Modify: `internal/store/note.go`（`CreateNote` / `UpdateNote` 的错误翻译；`SoftDeleteNote` 事务体抽成 `softDeleteNoteTx`）

**Interfaces:**
- Consumes: Task 1 的唯一索引、`normalizeTitle`、`maxContentRunes`、`nowUTC`、`recordActivityTx`、`GetNote`、`NewNoteID`、`buildNoteQuery`、`listSelectSQL`、`scanNoteItems`、`defaultCategoryID`。
- Produces（Task 6 依赖）：

```go
var (
    // ErrNoteTitleExists 表示同分类下已有同名笔记（撞唯一索引）。
    ErrNoteTitleExists = errors.New("note title already exists in this category")
    // ErrNoteTitleInTrash 表示同名笔记在回收站里：set/get/del 都不碰回收站。
    ErrNoteTitleInTrash = errors.New("note title is in trash")
)

// FindNoteByTitle 按 (分类, 标题 NOCASE) 取那唯一一条；回收站里的也返回（DeletedAt 非空）。
// categoryID 为 nil 表示未分类。
func (s *Store) FindNoteByTitle(ctx context.Context, categoryID *int64, title string) (Note, error)

// UpsertNoteByTitle 是 note set 的落库：命中活的 → 覆盖正文（rev+1、updated_at 刷新）；
// 命中加密笔记 → ErrEncryptedImmutable；命中回收站 → ErrNoteTitleInTrash；
// 都没有 → 新建。created 表示本次是新建。
func (s *Store) UpsertNoteByTitle(ctx context.Context, categoryID *int64, title, content string) (note Note, created bool, err error)

// SoftDeleteNoteByTitle 软删除那一条；已在回收站 → ErrNoteTitleInTrash；不存在 → ErrNoteNotFound。
func (s *Store) SoftDeleteNoteByTitle(ctx context.Context, categoryID *int64, title string) (Note, error)

// SoftDeleteCategoryNotes 软删除分类下所有未删除笔记，返回条数；每条记一次 note_delete。
func (s *Store) SoftDeleteCategoryNotes(ctx context.Context, categoryID *int64) (int, error)

// ListNotesInCategory 按 updated_at 倒序列出分类下未删除笔记（未分类传 DefaultCategoryID）。
func (s *Store) ListNotesInCategory(ctx context.Context, categoryID int64) ([]NoteListItem, error)
```

- [ ] **Step 1: 写失败的测试**

`internal/store/note_title_test.go`（新建，包内测试，可直接用 `s.db` 做断言）：

```go
func TestUpsertNoteByTitleCreatesThenOverwrites(t *testing.T)
func TestUpsertNoteByTitleRefusesTrashedTitle(t *testing.T)
func TestUpsertNoteByTitleRefusesEncryptedNote(t *testing.T)
func TestUpsertNoteByTitleMatchesCaseInsensitively(t *testing.T)
func TestSoftDeleteNoteByTitleTrashesOnce(t *testing.T)
func TestSoftDeleteCategoryNotesOnlyTouchesLiveNotes(t *testing.T)
func TestListNotesInCategoryOrdersByUpdatedAt(t *testing.T)
func TestCreateAndUpdateNoteMapTitleConflict(t *testing.T)
```

断言要点：

- 第一次 `Upsert` 返回 `created == true`、`note.Rev == 1`；第二次同标题 → `created == false`、同一个 `note.ID`、`Rev == 2`、`Content` 是第二次的正文、`updated_at` 变了；`activities` 里 `note_create` 1 条、`note_update` 1 条。
- 先 `SoftDeleteNote`，再 `Upsert` 同标题 → `errors.Is(err, ErrNoteTitleInTrash)`，且库里那条的 `content` 没被改。
- 加密笔记（`SetMasterPassword` + `NoteInput{CipherB64: ..., KDFParams: testKDFParams}`）→ `Upsert` 返回 `ErrEncryptedImmutable`。
- `Upsert("TODO")` 之后 `Upsert("todo")` → 同一个 id，`SELECT COUNT(*) FROM notes WHERE title = 'TODO' COLLATE NOCASE` == 1。
- `SoftDeleteNoteByTitle` 第一次成功（`DeletedAt != nil`），第二次 → `ErrNoteTitleInTrash`，不存在的标题 → `ErrNoteNotFound`。
- 分类 5 里 3 条活 + 1 条已删：`SoftDeleteCategoryNotes` 返回 3，事后 `SELECT COUNT(*) WHERE category_id = 5 AND deleted_at IS NULL` == 0；`activities` 里多 3 行 `note_delete`。未分类（nil）只影响 `category_id IS NULL` 的行。
- `ListNotesInCategory(DefaultCategoryID)` 只返回 `category_id IS NULL` 的活笔记，按 `updated_at` 倒序。
- `CreateNote` 同分类同名 → `ErrNoteTitleExists`；换成另一个分类 → 成功；`CreateNote` 用回收站里那条的标题 → `ErrNoteTitleInTrash`；`UpdateNote` 把 A 改成 B 的标题（同分类）→ `ErrNoteTitleExists`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'Title|CategoryNotes|NotesInCategory' -v`
Expected: 编译失败（`UpsertNoteByTitle` 未定义）。

- [ ] **Step 3: 实现 `internal/store/note_title.go`**

按标题定位的 SQL（`title` 列没有声明 `COLLATE`，必须显式写；分类为 nil 用 `IS NULL`）：

```sql
SELECT id FROM notes WHERE title = ? COLLATE NOCASE AND category_id IS NULL
SELECT id FROM notes WHERE title = ? COLLATE NOCASE AND category_id = ?
```

实现要点：

- `FindNoteByTitle`：先 `normalizeTitle`，再查 id，`GetNote` 回读；没行 → `ErrNoteNotFound`。
- `UpsertNoteByTitle`：`normalizeTitle` + 正文码点校验（空串合法，`> maxContentRunes` 返回 `ErrInvalidNote`）；一个 `inTx` 内 `SELECT id, is_encrypted, deleted_at`，按 Interfaces 里三种分支处理；新建走 `INSERT INTO notes (id, title, content, is_encrypted, cipher, kdf_salt, kdf_params, category_id, kind, remark, rev, created_at, updated_at) VALUES (?, ?, ?, 0, NULL, NULL, NULL, ?, 'note', '', 1, ?, ?)`；活动事件新建记 `ActivityNoteCreate`、覆盖记 `ActivityNoteUpdate`。
- `SoftDeleteNoteByTitle`：`inTx` 内查 id + `deleted_at`，不存在 → `ErrNoteNotFound`、已删 → `ErrNoteTitleInTrash`、活的 → 复用 `softDeleteNoteTx`。
- `SoftDeleteCategoryNotes`：`inTx` 内先 `SELECT id ... WHERE deleted_at IS NULL`，逐条 `UPDATE notes SET deleted_at = ? WHERE id = ?` 并 `recordActivityTx(..., ActivityNoteDelete)`，返回条数。
- `ListNotesInCategory`：复用 `buildNoteQuery(NoteFilter{CategoryID: categoryID})` + `listSelectSQL`，`scanNoteItems(rows, false)`。
- `note.go` 里把 `SoftDeleteNote` 的事务体抽成：

```go
// softDeleteNoteTx 只写 deleted_at；调用方负责确认目标是活的笔记。
func softDeleteNoteTx(ctx context.Context, tx *sql.Tx, id string) error
```

- `note.go` 增加错误翻译：

```go
// isTitleUniqueError 判断是不是撞上了标题唯一索引（错误文本里带 notes.title）。
func isTitleUniqueError(err error) bool

// classifyTitleConflict 在撞名之后探一次该 (分类, 标题) 的 deleted_at，
// 决定是「活着已有同名」还是「同名在回收站里」。
func (s *Store) classifyTitleConflict(ctx context.Context, categoryID *int64, title string) error
```

`CreateNote` / `UpdateNote` 在 `inTx` 返回 `isTitleUniqueError` 时（事务已回滚，连接可用）返回
`classifyTitleConflict(...)` 的结果。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/ -v`
Expected: PASS（全包，含 Task 1 的迁移测试）。

- [ ] **Step 5: 跑全量后端测试，修掉撞名的既有测试**

Run: `go test ./...`
Expected: PASS。既有测试里如果有「同分类两次用同一个标题建笔记」的地方，改成不同标题（不许为了过测试放宽索引）。

- [ ] **Step 6: CHANGELOG + 提交**

`[Unreleased] → Added` 追加「store 层按 (分类, 标题) 的 upsert/find/soft-delete 与 `ErrNoteTitleExists` / `ErrNoteTitleInTrash`」。

```bash
git add internal/store/ docs/CHANGELOG.md
git commit -m "feat(store): 按标题的 upsert/find/软删与标题冲突分类"
```

---

## Task 3: `cli.Ref` 寻址解析

**Files:**
- Create: `internal/cli/ref.go`
- Create: `internal/cli/ref_test.go`

**Interfaces:**
- Consumes: 无（纯字符串处理）。
- Produces（Task 4/5/6 依赖）：
  - `cli.QuickCategoryName = "速记"`
  - `type Ref struct { Title string; Category string; CategoryOnly bool }`
  - `func ParseRef(raw, typeFlag, defaultCategory string) (Ref, error)`
  - `const maxRefTitleRunes = 200`、`const maxRefCategoryRunes = 30`

- [ ] **Step 1: 写失败的测试**

`internal/cli/ref_test.go`（表驱动）：

| raw | typeFlag | 期望 |
|-----|----------|------|
| `周报` | `` | `{Title:"周报", Category:"速记"}` |
| `周报@工作` | `` | `{Title:"周报", Category:"工作"}` |
| `@工作` | `` | `{Category:"工作", CategoryOnly:true}` |
| `meeting@2024` | `工作` | `{Title:"meeting@2024", Category:"工作"}` |
| `a@b@c` | `` | `{Title:"a@b", Category:"c"}`（最后一个 `@` 切分） |
| `周报@` | `` | error |
| `@` | `` | error |
| `` | `` | error（空引用） |
| `＠` 之外的 200 码点标题 | `` | error（超长标题） |
| `周报@` + 31 码点分类 | — | error（分类名超长） |

另加 `TestParseRefTrimsWhitespace`：`" 周报 @ 工作 "` → `{Title:"周报", Category:"工作"}`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestParseRef -v`
Expected: 编译失败（`ParseRef` 未定义）。

- [ ] **Step 3: 实现 `internal/cli/ref.go`**

```go
const (
	maxRefTitleRunes    = 200
	maxRefCategoryRunes = 30
)

// Ref 是 <标题[@分类]> / @分类 的解析结果；Category 总是非空（默认落「速记」）。
type Ref struct {
	Title        string
	Category     string
	CategoryOnly bool
}
```

规则（顺序即优先级，注释写清「`--type` 是标题里要写 `@` 时的逃生口」）：

1. `typeFlag` 非空 → 整体当标题，`@` 不参与解析；
2. `raw` 以 `@` 开头 → 分类引用（`CategoryOnly`）；
3. 否则按**最后一个** `@` 切分标题与分类；
4. 都没有 → 标题取自 `raw`，分类取 `defaultCategory`。

校验：标题去空白后 1–200 码点（分类引用时标题必须为空）、分类去空白后 1–30 码点；错误返回
`fmt.Errorf("...（用 note set/get/del --help 查看用法）")`，由调用方映射成退出码 2。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run TestParseRef -v`
Expected: PASS。

- [ ] **Step 5: CHANGELOG + 提交**

```bash
git add internal/cli/ref.go internal/cli/ref_test.go docs/CHANGELOG.md
git commit -m "feat(cli): 标题@分类 寻址解析"
```

---

## Task 4: `cli.RunSet / RunGet / RunDel` 与输出格式

**Files:**
- Create: `internal/cli/quick.go`
- Create: `internal/cli/quick_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Ref`、既有 `cli.Category`、`cli.ErrNoteEncrypted`。
- Produces（Task 6 依赖）：

```go
// ErrQuickNotFound 表示按标题找不到笔记；ErrQuickInTrash 表示命名的那条在回收站里；
// ErrQuickCategoryNotFound 表示分类不存在。三者都由 cmd/note 翻译成中文提示。
var (
	ErrQuickNotFound         = errors.New("note not found by title")
	ErrQuickInTrash          = errors.New("note is in trash")
	ErrQuickCategoryNotFound = errors.New("category not found")
)

// QuickNote 是 CLI 需要的最小笔记形状。
type QuickNote struct {
	Title     string
	Content   string
	Encrypted bool
	Trashed   bool
	UpdatedAt string
}

// QuickDeps 是 note set/get/del 需要的持久层能力；cli 不依赖 store。
type QuickDeps struct {
	Categories     func(ctx context.Context) ([]Category, error)
	Category       func(ctx context.Context, name string) (Category, error)
	EnsureCategory func(ctx context.Context, name string) (Category, error)
	Set            func(ctx context.Context, category Category, title, content string) (QuickNote, bool, error)
	Get            func(ctx context.Context, category Category, title string) (QuickNote, error)
	Notes          func(ctx context.Context, category Category) ([]QuickNote, error)
	Delete         func(ctx context.Context, category Category, title string) (QuickNote, error)
	DeleteNotes    func(ctx context.Context, category Category) (int, error)
}

// 每个 Run* 返回要打印的文本；set/del 的返回值写 stderr，get 的写 stdout。
func RunSet(ctx context.Context, deps QuickDeps, ref Ref, content string) (string, error)
func RunGet(ctx context.Context, deps QuickDeps, ref Ref) (string, error)
func RunGetCategory(ctx context.Context, deps QuickDeps, ref Ref) (string, error)
func RunGetCategories(ctx context.Context, deps QuickDeps) (string, error)
func RunDel(ctx context.Context, deps QuickDeps, ref Ref) (string, error)
func RunDelCategory(ctx context.Context, deps QuickDeps, ref Ref) (string, error)
```

- [ ] **Step 1: 写失败的测试**

`internal/cli/quick_test.go`，用一个假 `QuickDeps`（记录调用、返回预置值、可注入错误）：

```go
func TestRunSetReportsCreatedAndUpdated(t *testing.T)   // "已保存「周报」@速记\n" / "已更新「周报」@速记\n"
func TestRunSetRejectsOversizedContent(t *testing.T)    // 2001 个字符 → error，不调用 deps.Set
func TestRunSetUsesDefaultCategory(t *testing.T)        // ref.Category=="速记" 时走 EnsureCategory（不预先查）
func TestRunGetReturnsContentWithTrailingNewline(t *testing.T) // "abc" → "abc\n"；"abc\n" → "abc\n"
func TestRunGetMapsTrashedAndEncrypted(t *testing.T)    // Trashed → ErrQuickInTrash；Encrypted → ErrNoteEncrypted
func TestRunGetCategoryFormatsRows(t *testing.T)        // 两行 "周报\t2026-11-02T08:00:00Z\n"
func TestRunGetCategoryEmptyIsBlankNotError(t *testing.T) // 空结果 → "" 且 err == nil
func TestRunGetCategoriesPrintsHeaderAndReserved(t *testing.T) // 首行 "ID\t分类\t笔记数\t备注"，固定分类备注列 "固定"
func TestRunDelReportsTrashAndMapsMissing(t *testing.T) // "已移入回收站「周报」@速记\n"；ErrQuickNotFound 透传
func TestRunDelCategoryReportsCount(t *testing.T)       // 3 → "已移入回收站 3 条（分类「工作」）\n"；0 → error
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestRun(Set|Get|Del)' -v`
Expected: 编译失败。

- [ ] **Step 3: 实现 `internal/cli/quick.go`**

要点（文案逐字，测试直接断言）：

- `maxQuickContentRunes = 2000`；`RunSet` 先校验正文码点数，超限返回错误（退出码 2 由 cmd 层映射），不调用 `deps.Set`。
- `RunSet`：`deps.EnsureCategory(ref.Category)`（这样 `标题@新分类` 顺带建分类）→ `deps.Set` → 按 `created` 选文案 `<已保存|已更新>「%s」@%s\n`。
- `RunGet`：`deps.Category` + `deps.Get`；`Trashed` → `ErrQuickInTrash`；`Encrypted` → `ErrNoteEncrypted`；正文不以 `\n` 结尾时补一个。
- `RunGetCategory`：`deps.Category` + `deps.Notes`；每行 `title + "\t" + UpdatedAt`；空结果返回 `""`。
- `RunGetCategories`：`text/tabwriter`（`minwidth=0 tabwidth=8 padding=2`），首行 `ID\t分类\t笔记数\t备注`，固定分类（`Reserved`）备注列写 `固定`，其余空串。
- `RunDel`：`deps.Category` + `deps.Delete`，文案 `已移入回收站「%s」@%s\n`。
- `RunDelCategory`：`deps.Category` + `deps.DeleteNotes`；`n == 0` 返回 `fmt.Errorf("分类「%s」下没有可删除的笔记", ref.Category)`；否则 `已移入回收站 %d 条（分类「%s」）\n`。
- 分类查找失败（`ErrQuickCategoryNotFound`）原样透传，由 cmd 层给中文。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: PASS。

- [ ] **Step 5: CHANGELOG + 提交**

```bash
git add internal/cli/quick.go internal/cli/quick_test.go docs/CHANGELOG.md
git commit -m "feat(cli): set/get/del 的纯逻辑与输出格式"
```

---

## Task 5: `parse.Parse` 认识 set / get / del + 帮助文案

**Files:**
- Modify: `internal/cli/parse.go`
- Modify: `internal/cli/usage.go`
- Modify: `internal/cli/parse_test.go`

**Interfaces:**
- Consumes: Task 3 的 `ParseRef`、`QuickCategoryName`。
- Produces（Task 6 依赖）：`CommandSet`、`CommandGet`、`CommandDel`（追加在 `CommandConfig` 之后、`CommandHelp` 之前不影响既有零值），以及 `Options.Set cli.SetOptions` / `Options.Get cli.GetOptions` / `Options.Del cli.DelOptions`：

```go
type SetOptions struct { Ref Ref; Content string }
type GetOptions struct { Ref Ref; List bool }
type DelOptions struct { Ref Ref; Yes bool }
```

- [ ] **Step 1: 写失败的测试**

`internal/cli/parse_test.go` 追加：

```go
func TestParseSetArgs(t *testing.T)     // {"set","周报","正文"} → Ref{Title:"周报",Category:"速记"}, Content:"正文"
func TestParseSetRejectsExtraArgs(t *testing.T)      // 三个位置参数 → error
func TestParseSetAcceptsTypeFlag(t *testing.T)       // {"set","--type","工作","周报","正文"}
func TestParseGetList(t *testing.T)                  // {"get","--list"} → List:true, Ref 零值
func TestParseGetRejectsListWithTarget(t *testing.T) // {"get","--list","@工作"} → error
func TestParseDelRequiresYesForCategory(t *testing.T) // {"del","@工作"} → error 文案含 "--yes"；{"del","@工作","--yes"} → Yes:true
func TestParseQuickCommandsAcceptConfigFlags(t *testing.T) // -c / --db 被消费进 Options.Args
func TestParseQuickCommandHelp(t *testing.T)         // {"set","--help"} → CommandHelp，主题 "set"
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestParse -v`
Expected: 失败（`CommandSet` 未定义 / 解析走错分支）。

- [ ] **Step 3: 实现**

`parse.go`：

- 三个新 `Command` 常量 + 三个 Options 结构体 + `Options` 三个字段；
- `case arg == "set" / "get" / "del"` 分支：先 `containsHelpFlag` → `opts.Args = []string{"set"}` 等，然后各自 `parseSetArgs / parseGetArgs / parseDelArgs`，签名与 `parseShowArgs` 一致 `(args []string) (XOptions, []string, error)`，配置参数（`-c/--config/--db`）原样透传；
- `set`：`--type <值>`；两个位置参数（目标、正文），多一个报错，少一个报「需要 <标题[@分类]> 与 <正文>」；
- `get`：`--type`、`--list`；位置参数 0 或 1 个，`--list` 与位置参数互斥；
- `del`：`--type`、`--yes`；位置参数恰好 1 个；如果解析结果是 `CategoryOnly` 且没有 `--yes` → 错误「删除整个分类需要显式加 --yes」；
- 三个分支都调 `ParseRef(raw, typeFlag, QuickCategoryName)`，错误包一层 `note <命令> ...` 前缀；
- 未识别命令的报错文案里补上 `set, get, del`。

`usage.go`：

- `Usage()` 命令表补三行（`set` 写/覆盖速记、`get` 读正文与列表、`del` 移入回收站）+ 三条示例；
- `UsageFor` 增加 `set` / `get` / `del` 主题；帮助里写明寻址语法（`标题` / `标题@分类` / `@分类` / `--type`）、退出码（0/1/2/4）、`del @分类` 必须 `--yes`、`set` 的提示走 stderr。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: PASS。

- [ ] **Step 5: CHANGELOG + 提交**

```bash
git add internal/cli/parse.go internal/cli/usage.go internal/cli/parse_test.go docs/CHANGELOG.md
git commit -m "feat(cli): set/get/del 子命令解析与帮助"
```

---

## Task 6: `cmd/note` 装配与退出码

**Files:**
- Create: `cmd/note/quick.go`
- Create: `cmd/note/quick_test.go`
- Modify: `cmd/note/main.go`（`run()` 的 switch 补三个分支）

**Interfaces:**
- Consumes: Task 2 的 store 方法、Task 4 的 `Run*` 与哨兵错误、Task 5 的 Options。
- Produces: `runSet(globalConfigPath string, args []string, opts cli.SetOptions) error` 等同名三个入口（`main.go` 只调用它们）。

- [ ] **Step 1: 写失败的测试**

`cmd/note/quick_test.go`（复用 `openTestStore` 的思路，直接对 temp 目录里的库跑 store + cli 组合）：

```go
func TestQuickSetThenGetRoundTrip(t *testing.T)      // set → 库里 1 条速记笔记；get 拿回原文
func TestQuickSetOverwritesSameTitle(t *testing.T)   // 两次 set 同名 → 库里仍 1 条，正文是第二次
func TestQuickDelMovesToTrash(t *testing.T)          // del → deleted_at 非空；再 get → ErrQuickInTrash 映射
func TestQuickCategoryNameConstantsMatch(t *testing.T) // store.QuickCategoryName == cli.QuickCategoryName
func TestQuickExitCodeForEncryptedGet(t *testing.T)  // 加密笔记 + get → showExitError 风格映射到 exitError(4)
```

断言方式：直接调 `quickDeps(st)` 的字段（与 `cmd/note` 内部同一份），或调 `cli.RunSet/RunGet`；退出码用
`errors.As(err, &exitError)` 后的值断言。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/note/ -run TestQuick -v`
Expected: 编译失败（`quickDeps` 未定义）。

- [ ] **Step 3: 实现 `cmd/note/quick.go`**

```go
// quickDeps 把 store 包装成 cli.QuickDeps；store 的哨兵错误在这里翻译成 cli 的哨兵错误。
func quickDeps(st *store.Store) cli.QuickDeps

// categoryID 把 cli.Category 落成库里用的 category_id：未分类必须是 NULL。
func categoryID(category cli.Category) *int64
```

映射规则：

| store | cli |
|-------|-----|
| `ErrNotFound`（分类） | `cli.ErrQuickCategoryNotFound` |
| `ErrNoteNotFound` | `cli.ErrQuickNotFound` |
| `ErrNoteTitleInTrash` | `cli.ErrQuickInTrash` |
| `ErrEncryptedImmutable` | `cli.ErrNoteEncrypted` |

三个入口函数：`loadConfig` → `store.Open` → 调 `cli.Run*` → 打印：

```go
func runSet(globalConfigPath string, args []string, opts cli.SetOptions) error
func runGet(globalConfigPath string, args []string, opts cli.GetOptions) error  // --list 走 RunGetCategories
func runDel(globalConfigPath string, args []string, opts cli.DelOptions) error  // CategoryOnly 走 RunDelCategory
```

打印与退出码（用既有 `exitError` 类型）：

- 成功：`set`/`del` 的文本写 `os.Stderr`；`get` 的文本写 `os.Stdout`；
- `cli.ErrQuickNotFound` → stderr `找不到标题为「%s」的笔记`，`exitError(1)`；
- `cli.ErrQuickInTrash` → stderr `「%s」在回收站里，先在 note web 里恢复或彻底删除`，`exitError(1)`；
- `cli.ErrNoteEncrypted` → `get` 走 `exitError(4)`（文案 `「%s」是加密笔记，CLI 没有主密码，无法解密`）；`set` 走 `exitError(1)`（文案 `「%s」是加密笔记，CLI 不能覆盖`）；
- `cli.ErrQuickCategoryNotFound` → stderr `分类「%s」不存在`，`exitError(1)`；
- 其它错误原样返回（`main` 打 `startup failed`、退出码 1）；用法错误（`ParseRef`/内容超限/没删到）→ `exitError(2)`。

`main.go` 的 switch 追加：

```go
case cli.CommandSet:
	return runSet(opts.ConfigPath, opts.Args, opts.Set)
case cli.CommandGet:
	return runGet(opts.ConfigPath, opts.Args, opts.Get)
case cli.CommandDel:
	return runDel(opts.ConfigPath, opts.Args, opts.Del)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./cmd/note/ -v && go build ./...`
Expected: PASS。

- [ ] **Step 5: 手工冒烟（临时库）**

```bash
go run ./cmd/note set --db /tmp/q.db 周报 "本周结论"
go run ./cmd/note get --db /tmp/q.db 周报
go run ./cmd/note set --db /tmp/q.db 周报@工作 "换了分类"
go run ./cmd/note get --db /tmp/q.db @工作
go run ./cmd/note get --db /tmp/q.db --list
go run ./cmd/note del --db /tmp/q.db @工作      # 应报错：需要 --yes
go run ./cmd/note del --db /tmp/q.db @工作 --yes
```

Expected：`get 周报` 输出 `本周结论`；`get @工作` 一行；`--list` 里「未分类」和「速记」备注列都是 `固定`；不带 `--yes` 的那条退出码 2。

- [ ] **Step 6: CHANGELOG + 提交**

```bash
git add cmd/note/ docs/CHANGELOG.md
git commit -m "feat(cli): 接入 note set/get/del 与退出码"
```

---

## Task 7: 网页端 409 映射 + 前端 `reserved`

**Files:**
- Modify: `internal/web/handlers_notes.go`（`writeNoteStoreError`）
- Modify: `internal/web/notes_api_test.go`
- Modify: `web/src/lib/notes.ts`（`Category` 加 `reserved: boolean`）
- Modify: `web/src/components/CategoryTree.svelte`（去掉硬编码 `DEFAULT_CATEGORY_ID`）
- Modify: `web/src/components/CategoryTree.test.ts`

**Interfaces:**
- Consumes: Task 2 的 `store.ErrNoteTitleExists` / `store.ErrNoteTitleInTrash`、Task 1 的 `Category.Reserved`。
- Produces: HTTP 409 码 `title_conflict`（「同分类下已有同名笔记」）与 `title_in_trash`（「回收站里已有同名笔记，请先恢复或彻底删除」）。

- [ ] **Step 1: 写失败的测试**

`internal/web/notes_api_test.go` 追加：

```go
func TestCreateNoteDuplicateTitleInCategoryConflicts(t *testing.T) // 第二次同分类同名 → 409 + error.code == "title_conflict"
func TestCreateNoteWithTrashedTitleConflicts(t *testing.T)         // 删掉后重建同名 → 409 + code == "title_in_trash"
func TestRenameNoteOntoExistingTitleConflicts(t *testing.T)        // PUT 改名撞名 → 409 + "title_conflict"
func TestCategoryTreeMarksReserved(t *testing.T)                   // GET /api/categories 里 id=1、id=2 的 reserved 都是 true，其它 false
```

`web/src/components/CategoryTree.test.ts` 追加：`reserved: true` 的分类不渲染「改名」「删除」按钮（
用既有测试的 mount 方式，断言按钮文案不存在）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run 'Conflicts|Reserved' -v` 与 `npm --prefix web run test -- --run CategoryTree`
Expected: 409 断言失败（现状是 500 `internal` 或 201）；前端按钮仍出现。

- [ ] **Step 3: 实现**

`writeNoteStoreError` 在 `ErrNoteNotFound` 分支之前加：

```go
case errors.Is(err, store.ErrNoteTitleInTrash):
	writeError(w, http.StatusConflict, "title_in_trash", "回收站里已有同名笔记，请先恢复或彻底删除")
case errors.Is(err, store.ErrNoteTitleExists):
	writeError(w, http.StatusConflict, "title_conflict", "同分类下已有同名笔记")
```

（顺序重要：`ErrNoteTitleInTrash` 与 `ErrNoteTitleExists` 是两条不同哨兵，互不包含，但把回收站那条放前面读起来更清楚。）

前端：

- `notes.ts`：`Category` 加 `reserved: boolean;`
- `CategoryTree.svelte`：删除 `const DEFAULT_CATEGORY_ID = 1`，`{#if category.id !== DEFAULT_CATEGORY_ID}` 改为 `{#if !category.reserved}`，注释改成「固定分类（未分类 / 速记）后端改名删除都 409，界面不给按钮」。
- 若前端别处（`NoteListView`/`NoteEditorView`）构造 `Category` 字面量，补 `reserved: false` 以保证类型通过 `svelte-check`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -v && npm --prefix web run test -- --run`
Expected: PASS。

- [ ] **Step 5: CHANGELOG + 提交**

```bash
git add internal/web/ web/src/ docs/CHANGELOG.md
git commit -m "feat(web): 同名标题 409 与固定分类 reserved 标记"
```

---

## Task 8: 文档收口与全量验证

**Files:**
- Modify: `docs/PRD.md`（`5.1.2 功能列表`、`5.4 页面与接口清单`、`5.5 分类管理`、`6.7 分类树`）
- Modify: `docs/TECH_DESIGN.md`（`5.1 Schema`、`5.3 迁移`、`5.4 查询与搜索`、`8.5 校验与状态映射`、`4.x` 增补 CLI 一节）
- Modify: `AGENTS.md`（项目概览三条命令、硬性约定两条）
- Modify: `docs/CHANGELOG.md`（补齐本功能的 Added / Changed 全貌）

- [ ] **Step 1: 更新 `AGENTS.md`**

- 「项目概览」补：`note set / get / del` 用 `标题[@分类]` 寻址、`note set` 是覆盖语义、`del` 是软删除；
- 「硬性约定」补两条：①同分类标题唯一（唯一索引包含回收站行，`set` 撞回收站同名是拒绝而非复活）；②「速记」（id=2）是第二个固定分类，守卫与「未分类」同源，前端只认 `reserved` 字段、不硬编码 id。

- [ ] **Step 2: 更新 `docs/PRD.md`**

- `5.1.2`：CLI 侧三条命令的一句话描述与退出码；
- `5.4`：新增 409 `title_conflict` / `title_in_trash`；
- `5.5`：速记是第二个固定分类（不可改名/删除）；
- `6.7`：固定分类不给改名/删除按钮（前端读 `reserved`）。

- [ ] **Step 3: 更新 `docs/TECH_DESIGN.md`**

- `5.1`：`categories` 种子行、两条唯一索引的 DDL 与「包含回收站行」的理由；
- `5.3`：0007 的去重策略（分组键、保留顺序、后缀规则、200 字符截断、失败回滚）；
- `5.4`：按标题查询必须 `COLLATE NOCASE`（列本身无 COLLATE）、未分类是 `IS NULL`；
- `8.5`：两个 409 码的映射；
- 新增小节：`note set/get/del` 的寻址语法、默认分类、stderr/stdout 约定、退出码表。

- [ ] **Step 4: 全量验证**

Run: `just test`
Expected: `go test ./...` 与 Vitest 全绿。

Run: `just check`
Expected: `go vet` 与 `svelte-check`/`tsc` 无错误。

- [ ] **Step 5: CHANGELOG 收口 + 提交**

确认 `[Unreleased]` 已按 Added / Changed 分类覆盖全部改动（速记分类、三个命令、迁移、409、前端 reserved、文档），且没有把未验证项写成已完成。

```bash
git add docs/ AGENTS.md
git commit -m "docs: 速记分类与 set/get/del 的 PRD/技术方案收口"
```
