# 速记分类与 CLI `set / get / del` 设计

日期：2026-11-02
状态：待评审（需要用户批准后才进入任务拆分与实现）

## 1. 背景与意图

现在是「网页为主、CLI 为辅」：`note add` 只能在终端里走三步向导，写一条笔记至少按三次键；
想改其中一条、只想看看某条正文、想删掉某条，CLI 都没有入口（只能 `note list` 里翻、`note show <id>` 看）。

真正的痛点是**终端里的高频短笔记**：一句命令记一条、一句命令读回来。这类笔记不值得分类、不值得打开网页、
不值得记住 UUID。所以引入：

1. 一个固定分类 **「速记」**（id=2，与「未分类」同为内置、不可改名、不可删除），作为 `note set` 的默认落点；
2. 三个非交互命令 `note set / get / del`，用 **标题**（而不是 UUID）寻址，用 `标题@分类` / `@分类` 表达分类范围；
3. `del` 沿用既有的软删除（进回收站），不做物理删除。

标题寻址要成立，就必须回答「同一个分类里两条同名笔记算谁」——本设计把它定为**同一分类内标题唯一**
（用户已确认：`set` 遇到同名是**覆盖**）。

## 2. 目标

- `note set 标题 正文` 一句命令创建/覆盖「速记」里的一条明文笔记，不需要打开网页、不需要交互。
- `note set 标题@分类 正文` 在指定分类里做同样的事；分类不存在时自动新建（分类名是用户自己的命名空间）。
- `note get 标题[@分类]` 把正文原样写到 stdout（`get` 是管道/脚本友好的，不渲染、不碰剪贴板）。
- `note get @分类` 列出该分类下所有未删除笔记的标题与更新时间。
- `note get --list` 列出所有分类（含笔记数、固定分类标记）。
- `note del 标题[@分类]` 软删除单条；`note del @分类 --yes` 软删除整个分类下的笔记。
- 网页端与 CLI 对「同分类同名」的理解一致：撞名是 409，不是 500，也不是静默覆盖。

## 3. 非目标

- 不做 `note edit` / `$EDITOR` 集成（`set` 已经覆盖了「改内容」）。
- 不做 CLI 的回收站恢复/彻底删除（仍走网页；`del` 只说清「进了回收站」）。
- 不引入新的 `kind` 取值：速记笔记仍然是 `kind = 'note'`，分类才是唯一标识（网页不认 `kind`，两套标识会分叉）。
- 不做 `note set` 的交互补全、不做标题 Tab 补全。
- 不给 `set` 加 stdin（`-`）输入；多行正文用 shell 的引号就够了，`get` 才是管道入口。
- 不把加密笔记的正文搬进 CLI（CLI 没有主密码，永远解不开）。

## 4. 反目标（即使能跑起来也算失败）

- 因为新增唯一约束，让网页端「建一条同名笔记」从能用变成 500。
- `note set` 静默覆盖回收站里的同名笔记，把用户后悔药里那条的正文冲掉。
- `note del @分类` 在没有防护的情况下被一次误输入清空整个分类。
- 让 `note get` 把标题、分类、编译期提示混进 stdout，导致脚本拿到脏数据。

## 5. 约束（来自既有代码的事实）

- 时间戳统一 `2006-01-02T15:04:05Z`；库结构变更只能新增 `sql/NNNN_*.sql`。
- 分类：`categories(id, name UNIQUE COLLATE NOCASE, created_at)`；id=1「未分类」由应用层守卫
  （`store.ErrReservedCategory`），未分类的笔记在库里存 `category_id IS NULL`（`store.DefaultCategoryID` ↔ NULL）。
- 笔记：`notes` 有 `deleted_at`（软删除）、`rev`、`updated_at`，`UNIQUE` 目前只有主键 `id`；
  网页端同一分类下允许任意多条同名笔记。
- 迁移在 `store.migrate()` 里整文件 `Exec`（SQL 只能写 SQL，没有 Go 钩子），每个迁移一个事务。
- CLI 解析在 `internal/cli/parse.go`（手写），命令实现在 `internal/cli`（可测的纯逻辑）+ `cmd/note`（装配 store/终端）。
- 正文上限 2000 码点、标题 1–200 码点、分类名 ≤30 码点，三处（store/web/cli）各自有常量，语义同值。
- 「查看」事件下沉在 `store.ViewNote`；`GetNote` 是内部回读，不记事件。
- 网页错误映射集中在 `internal/web/handlers_notes.go#writeNoteStoreError`。

## 6. 已定的三个取向（用户 2026-11-02 选择）

| 分叉               | 选择                  | 含义                                                                              |
| ------------------ | --------------------- | --------------------------------------------------------------------------------- |
| 标题唯一性         | **①B 数据库唯一索引** | 加 `UNIQUE(分类, 标题)`；网页端同名从「允许」改为 409；需要一条清理存量重名的迁移 |
| `del @分类` 安全阀 | **②B 必须 `--yes`**   | 整分类删除只在显式加 `--yes` 时执行，否则是用法错误（退出码 2）                   |
| `get` 输出形态     | **③A 只输出正文**     | `get 标题` 输出原始 Markdown；`get @分类` 一行一条；不渲染、不复制、不打印头部    |

## 7. 设计

### 7.1 固定分类「速记」

- 名字：**速记**（两字、口语、中性；与「未分类」同为短名）。
- id 固定为 2，由迁移 `0007` 插入，`created_at` 与 0004 的种子行同风格写死常量。
- `store` 新增 `QuickCategoryID = 2` 与 `IsReservedCategory(id) bool`（`1 || 2`）；
  `RenameCategory` / `DeleteCategory` 的守卫从 `id == defaultCategoryID` 改成 `IsReservedCategory(id)`，
  仍返回 `ErrReservedCategory`（网页 409、CLI 文案不变）。
- `store.Category` 增加 `Reserved bool`（JSON `reserved`），由 `ListCategories` 在 Go 里按 id 算出来；
  网页 `CategoryTree.svelte` 不再硬编码 `DEFAULT_CATEGORY_ID = 1`，改用 `!category.reserved` 决定是否显示改名/删除按钮
  （前端类型 `web/src/lib/notes.ts#Category` 同步加 `reserved`）。
- `note add` 的分类列表照常出现「速记」；`IsDefault` 语义不变（只有 id=1 映射成 `category_id = NULL`）。

### 7.2 寻址语法

三种引用形状，解析集中在 `internal/cli`：

| 写法          | 含义                                                      |
| ------------- | --------------------------------------------------------- |
| `标题`        | 速记分类里的这条标题                                      |
| `标题@分类`   | 指定分类里的这条标题                                      |
| `@分类`       | 整个分类（只用于 `get` / `del`）                          |
| `--type 分类` | 显式分类；给了它就不再拆 `@`（标题里要写 `@` 时的逃生口） |

规则：

1. `--type` 非空时，位置参数整体当作标题，`@` 不参与解析。
2. 位置参数以 `@` 开头 → 分类引用，标题为空；`@` 后面为空是用法错误。
3. 否则在**最后一个** `@` 处切成 `标题` + `分类`；任一半边为空是用法错误。
4. 没有 `@` 且没有 `--type` → 分类取「速记」。

校验：标题 1–200 码点（`set` 必填，`get`/`del` 必填或整段为空）；分类名 ≤30 码点，与网页
（`validateCategoryName`）和 `note add`（`maxAddCategoryRunes`）同口径。分类名比较交给 SQLite 的
`NOCASE` 列（与网页建分类的撞名判定一致），Go 侧不再做第二套大小写规则。

解析结果是一个小结构体 `cli.Ref{Title string; Category string; CategoryOnly bool}`，
`internal/cli` 内的 `RunSet/RunGet/RunDel` 只吃这个结构体与注入的 `Deps`，不碰 store，便于单测。

### 7.3 `note set`

```
note set <标题[@分类]> <正文> [--type 分类] [-c|--config 路径] [--db 路径]
```

- 一次事务里按 `(分类, 标题)` 找**唯一**的那一行（唯一索引保证最多一条，包括回收站里的）：
  - 命中且未删除 → `UPDATE` 正文（`rev+1`、`updated_at=now`），记一次 `note_update`；
  - 命中但在回收站 → **拒绝**并把笔记 id 报出来：`同名笔记在回收站里（id …），先在网页恢复或彻底删除`，
    退出码 1。理由：`set` 的「覆盖」是对活跃笔记说的，静默复活并冲掉回收站里那条正文属于数据丢失；
  - 未命中 → 新建，记一次 `note_create`。
- 分类不存在时**自动创建**（`note set 标题@新分类 正文` 就是「顺带建分类」）；分类为「未分类」时落 `category_id = NULL`。
- 命中但那条是**加密笔记** → 拒绝（`set` 只会写明文，加密后不可回退；与 `store.ErrEncryptedImmutable` 同源），
  退出码 1，提示去网页处理。
- 正文允许空串（0–2000 码点），用来清空内容；超过 2000 码点报错，不截断。
- stdout 一个字节都不打印；成功提示写到 stderr（`已保存「标题」@速记` / `已更新「标题」@速记`），
  这样 `note set … && note get …` 之类的脚本管道不被污染。

### 7.4 `note get`

```
note get <标题[@分类]>     # 输出正文
note get @分类             # 列出分类下的笔记
note get --list            # 列出所有分类
```

- `get 标题[@分类]`：读**未删除**的那条（走 `store.ViewNote`，与 `note show` / 网页详情页同一套「查看」事件口径），
  把 `content` 原样写 stdout；正文不以换行结尾时补一个换行（便于终端与 `wc -l`），其它字节不动。
  找不到 → stderr + 退出码 1；同名笔记只在回收站里 → stderr 说明 + 退出码 1；
  加密笔记 → stderr 说明 CLI 解不开 + 退出码 4（与 `note show` 的「看不了」一致）。
- `get @分类`：只列未删除的，按 `updated_at` 倒序，每行 `标题\tupdated_at`（tab 分隔，便于 `cut`）；
  加密笔记照样出现（标题是明文）；分类下没有笔记 → 不输出任何内容、退出码 0（与 `note list --plain` 一致）；
  分类不存在 → stderr + 退出码 1。
- `get --list`：列表头一行 `ID\t分类\t笔记数\t备注`，其后每行一条分类；固定分类（未分类 / 速记）在备注列写 `固定`。
  用 `text/tabwriter` 对齐，与 `cli.FormatListPlain` 同风格。
- `--list` 与位置参数同时出现是用法错误。

### 7.5 `note del`

```
note del <标题[@分类]>              # 软删除一条
note del @分类 --yes                # 软删除分类下所有未删除笔记
```

- 单条：`UPDATE notes SET deleted_at = now`（复用 `SoftDeleteNote` 的事务体），记一次 `note_delete`；
  成功提示走 stderr（`已移入回收站「标题」@速记`），stdout 为空。
  找不到 → 退出码 1；已在回收站 → 退出码 1 且文案说清「已经在回收站里」。
- 整分类：必须显式 `--yes`，否则用法错误（退出码 2，提示补 `--yes`）。
  软删除该分类下所有未删除笔记，每条记一次 `note_delete`，stderr 打印 `已移入回收站 N 条（分类「x」）`；
  N = 0 时是「没有可删的笔记」，退出码 1（脚本能区分「删到了」与「没删到」）。
- 加密笔记照常可删（软删除不碰正文/密文）。
- `del` 不支持 `--list`。

### 7.6 退出码

| 码  | 含义                                                               |
| --- | ------------------------------------------------------------------ |
| 0   | 成功（含 `get @分类` 空结果）                                      |
| 1   | 找不到、只在回收站里、加密看不了以外的失败、`del` 没有删到任何东西 |
| 2   | 用法错误（参数形状、缺 `--yes`、标题/分类名校验失败）              |
| 4   | `get` 命中的是加密笔记（CLI 没有主密码）                           |

### 7.7 迁移 `sql/0007_quick_category_and_unique_title.sql`

一个事务内三步：

1. **插入速记分类**：`INSERT INTO categories (id, name, created_at) VALUES (2, '速记', '2000-01-01T00:00:00Z');`
   （id 与 0004 的种子同风格写死；`name` 的 NOCASE 唯一约束保证不会与用户已有分类撞名——万一撞了，迁移在事务内失败，
   库不变、`user_version` 不前进，用户改名后重启即可。）

2. **清理存量重名**（唯一索引落不下去的根因）：按 `(category_id, 标题 NOCASE)` 分组，每组保留一条，
   其余改名。保留顺序：`deleted_at IS NULL` 优先（活着的笔记保住原名）→ `updated_at` 最新 → `id` 最大。
   改名规则：追加 ` (2)`、` (3)`…（序号取组内 `ROW_NUMBER()`），若该名字在同分类里已被占用，
   退化为追加 ` (2 · <id 前 8 位>)`；组内序号唯一 + id 唯一，保证改名结果两两不同。
   改名后超过 200 字符的按 200 截断（应用层的长度守卫，DB 不强制）。

   落地形状（迁移里只能写 SQL，没有 Go 钩子；序号与兜底两条路径都要有注释）：

   ```sql
   WITH ordered AS (
       SELECT id, title, category_id,
              ROW_NUMBER() OVER (
                  PARTITION BY category_id, title COLLATE NOCASE
                  ORDER BY (deleted_at IS NULL) DESC, updated_at DESC, id DESC
              ) AS rn
       FROM notes
   ),
   plan AS (
       SELECT o.id, o.title, o.category_id, o.rn,
              o.title || ' (' || o.rn || ')' AS candidate
       FROM ordered o WHERE o.rn > 1
   ),
   final AS (
       SELECT p.id,
              CASE WHEN EXISTS (
                       SELECT 1 FROM notes x
                       WHERE x.id <> p.id
                         AND (x.category_id IS p.category_id)
                         AND x.title = p.candidate COLLATE NOCASE
                   )
                   THEN substr(p.title, 1, 200 - length(' (' || p.rn || ' · ' || substr(p.id, 1, 8) || ')'))
                        || ' (' || p.rn || ' · ' || substr(p.id, 1, 8) || ')'
                   ELSE substr(p.title, 1, 200 - length(' (' || p.rn || ')')) || ' (' || p.rn || ')'
              END AS new_title
       FROM plan p
   )
   UPDATE notes SET title = (SELECT new_title FROM final WHERE final.id = notes.id)
   WHERE id IN (SELECT id FROM final);
   ```

   注意 `x.category_id IS p.category_id` 用的是 SQLite 的 `IS`（NULL 与 NULL 相等），不能用 `=`；
   兜底分支带 `id` 前 8 位，理论上的碰撞只剩「用户恰好手写了一个同名标题」这种天文概率，
   真撞上时 `CREATE UNIQUE INDEX` 在同一个事务里失败 → 整个迁移回滚、`user_version` 不前进、库不变。
   这条语句必须有专门的迁移测试（见 §9），不接受「看起来对」。

3. **建两个唯一索引**（`category_id IS NULL` 那组需要单独一条 partial index，因为 SQL `NULL` 在普通唯一索引里互不相等）：

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_category
    ON notes(title COLLATE NOCASE, category_id) WHERE category_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_uncategorized
    ON notes(title COLLATE NOCASE) WHERE category_id IS NULL;
```

索引**包含回收站里的行**（不带 `deleted_at` 谓词）。理由见 7.8。

### 7.8 唯一索引包含回收站：为什么，以及代价

- 好处：`restore` 永远不会撞名。若索引排除 `deleted_at IS NOT NULL`，会出现「删掉『周报』→ 新建『周报』→ 想恢复旧的」
  的撞名死局，而回收站里的笔记目前不可编辑，用户没有解除撞名的手段。
- 代价：网页端用回收站里同名笔记的标题新建会被拒绝。这不是 500 也不是静默成功，而是
  **409 `title_in_trash`**，文案指明「回收站里已有同名笔记，请先恢复或彻底删除」——用户有明确出路。
- `note set` 同样遵守这条：命中回收站 → 拒绝（7.3），不复活、不覆盖。

### 7.9 store 层

新增（`internal/store/note.go` 或新的 `note_title.go`）：

```go
var (
    ErrNoteTitleExists = errors.New("note title already exists in this category")
    ErrNoteTitleInTrash = errors.New("note title is in trash")
)

// 按 (分类, 标题) 查未删除的一条；分类为 nil 表示未分类。
FindNoteByTitle(ctx, categoryID *int64, title string) (Note, error)
// set 的落库：命中更新 / 命中回收站返回 ErrNoteTitleInTrash / 未命中新建。
// created 表示本次是新建。
UpsertNoteByTitle(ctx, categoryID *int64, title, content string) (note Note, created bool, err error)
// 单条软删除；命中回收站返回 ErrNoteTitleInTrash。
SoftDeleteNoteByTitle(ctx, categoryID *int64, title string) (Note, error)
// 整分类软删除，返回删除条数。
SoftDeleteCategoryNotes(ctx, categoryID *int64) (int, error)
// 按 updated_at 倒序列出分类下的未删除笔记（未分类传 DefaultCategoryID，
// buildNoteQuery 会把它展开成 category_id IS NULL）。
ListNotesInCategory(ctx, categoryID int64) ([]NoteListItem, error)
// 按名字取分类（NOCASE）；不存在返回 ErrNotFound。
CategoryByName(ctx, name string) (Category, error)
// 按名字取分类，不存在则新建（给 `note set 标题@分类` 用）。
EnsureCategory(ctx, name string) (Category, error)
```

- `CreateNote` / `UpdateNote`：`INSERT`/`UPDATE` 撞上唯一索引时，把错误翻译成
  `ErrNoteTitleExists`；但若撞的是回收站里的那一行，`classifyTitleConflict` 探一次 `deleted_at` 后返回
  `ErrNoteTitleInTrash`。主键冲突仍保持现状（网页 `id_conflict`）。
- 按标题的查询必须显式写 `title = ? COLLATE NOCASE`：`notes.title` 列在 0001 里**没有**声明 `COLLATE`
  （列默认 BINARY），NOCASE 只写在索引定义里；不显式加 `COLLATE NOCASE` 时 `WHERE title = ?` 是区分大小写的，
  行为会与唯一约束（不区分大小写）不一致。`categories.name` 相反，列上就带 NOCASE，查询不必再写。
- 标题唯一性错误的判定靠 SQLite 错误文本（`UNIQUE constraint failed: notes.title` 与
  `..., notes.category_id` 两种形态），与既有 `isUniqueConstraint` 同一套做法；命中后
  `classifyTitleConflict` 再探一次该 (分类, 标题) 的 `deleted_at`，决定返回 `ErrNoteTitleExists` 还是
  `ErrNoteTitleInTrash`。
- `SoftDeleteNote` 的事务体抽成 `softDeleteNoteTx(ctx, tx, id, now)`，供按标题/按分类两条路径复用。

### 7.10 web 层

- `writeNoteStoreError` 增加两个分支：
  - `ErrNoteTitleExists` → 409 `title_conflict`「同分类下已有同名笔记」；
  - `ErrNoteTitleInTrash` → 409 `title_in_trash`「回收站里已有同名笔记，请先恢复或彻底删除」。
- 前端不改交互：`ApiRequestError.message` 已经会显示到编辑页；分类树改用 `reserved` 字段。
- 其余 API（列表、搜索、回收站、加密、改密）零改动。

### 7.11 统计事件口径

| 命令                       | 事件                                                        |
| -------------------------- | ----------------------------------------------------------- |
| `set` 新建                 | `note_create`                                               |
| `set` 覆盖                 | `note_update`                                               |
| `get 标题`                 | `note_view`（走 `ViewNote`，与网页详情页/`note show` 同源） |
| `get @分类` / `get --list` | 不记（与 `note list` 一致）                                 |
| `del 标题` / `del @分类`   | 每条被删笔记一次 `note_delete`                              |

## 8. 影响面

| 位置                                                             | 改动                                                                                                       |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `sql/0007_*.sql`                                                 | 新增：速记分类、存量重名清理、两条唯一索引                                                                 |
| `internal/store/category.go`                                     | `QuickCategoryID`、`IsReservedCategory`、`Category.Reserved`、`CategoryByName`、`EnsureCategory`、守卫改写 |
| `internal/store/note.go`（或新文件）                             | 四个按标题的读写、错误翻译与 `classifyTitleConflict`、`softDeleteNoteTx` 抽取                              |
| `internal/cli/ref.go`（新）                                      | `Ref` 解析与校验                                                                                           |
| `internal/cli/quick.go`（新）                                    | `RunSet/RunGet/RunDel/RunCategoryList` + 输出格式化 + `Deps`                                               |
| `internal/cli/parse.go`                                          | `CommandSet/Get/Del`、`SetOptions/GetOptions/DelOptions`、`--type/--yes/--list`                            |
| `internal/cli/usage.go`                                          | 总帮助三条命令 + `set/get/del` 三个主题帮助                                                                |
| `cmd/note/quick.go`（新）+ `main.go`                             | 三个子命令的装配、退出码映射                                                                               |
| `internal/web/handlers_notes.go`                                 | 两个新的 409 映射                                                                                          |
| `web/src/lib/notes.ts`、`web/src/components/CategoryTree.svelte` | `reserved` 字段替代硬编码 id                                                                               |
| 测试                                                             | store（唯一性/upsert/软删/迁移去重）、cli（Ref/格式化）、parse、web（409 两种）                            |
| 文档                                                             | `docs/PRD.md`、`docs/TECH_DESIGN.md`、`AGENTS.md` 硬性约定、`CHANGELOG.md`                                 |

文档改动落点（避免整篇重写）：

- `docs/PRD.md`：`5.1.2 功能列表`（CLI 入口三条）、`5.5 分类管理`（速记是第二个固定分类）、
  `5.4 页面与接口清单`（新增 409 码）、`6.7 分类树`（固定分类不给改名/删除按钮）。
- `docs/TECH_DESIGN.md`：`5.1 Schema`（新增唯一索引与速记行）、`5.3 迁移`（0007 的去重策略）、
  `5.4 查询与搜索`（按标题查询的 NOCASE 与 NULL 分类）、`8.5 校验与状态映射`（两个 409 码）、
  以及 `4.x` 里补一节 CLI `set/get/del` 的寻址语法与退出码。
- `AGENTS.md`：在「项目概览」补三条命令、在「硬性约定」补「同分类标题唯一（索引包含回收站）」与
  「速记是固定分类，守卫与未分类同源」。

## 9. 测试计划

- **迁移**：造一个 `user_version = 6` 的库（依次执行 0001–0006 的 SQL），插入同分类同名（含大小写变体、
  含一条在回收站里、含「周报 (2)」这种预占后缀）的笔记，然后 `store.Open` → 断言迁移成功、
  每组只剩一个原名的活笔记、所有名字仍唯一、200 字符上限被截断、速记分类存在。
- **store**：upsert 新建/覆盖/命中回收站拒绝/加密笔记拒绝；`SoftDeleteNoteByTitle` 的三条分支；
  `SoftDeleteCategoryNotes` 的条数与未分类（NULL）分类；`EnsureCategory` 的并发/已存在路径；
  `CreateNote`/`UpdateNote` 撞名 → 两个哨兵错误。
- **cli**：`Ref` 解析表驱动（无 `@`、`标题@分类`、`@分类`、`--type` 覆盖、`a@b@c`、`@`、`标题@`、长度校验）；
  `get @分类` / `get --list` 的输出逐字节断言。
- **parse**：三个新命令的参数形状、`--yes` 缺失、`--list` 与位置参数互斥、帮助主题。
- **web**：同名新建/改名 → 409 `title_conflict`；用回收站同名标题新建 → 409 `title_in_trash`；
  速记分类的改名/删除 → 409 `reserved_category`（复用既有 `ErrReservedCategory` 分支）。
- **前端**：`CategoryTree` 的 `reserved` 行为（改名/删除按钮不出现）。
- `just test` + `just check` 全绿后才写 CHANGELOG。

## 10. 留给 builder 的细节（我的默认取舍，可在实现时调整）

1. `note set` 命中回收站是**拒绝**而不是复活覆盖（7.3/7.8）——选它是因为静默冲掉回收站正文属于数据丢失。
2. 唯一索引**包含回收站**（7.8），代价是网页端会多一个 `title_in_trash` 409。
3. 标题比较用 `NOCASE`（与分类名一致）；`README` 与 `readme` 在 CLI 眼里是同一条。
4. 存量重名按「活的优先 → 新的优先」保留原名，其余追加 ` (2)` 序号；迁移里不留「静默丢失」。
5. `set` 的成功提示走 stderr，stdout 保持空——`note get` 是这套命令里唯一的 stdout 使用者（除 `get @分类`/`--list`）。
6. `get @分类` 默认只给标题（v1.15 修订：时间与正文挪到 `--detail` 表格里），不带 id：`del`/`get` 都按标题寻址，id 在这里没有用处。
7. 「速记」是否出现在网页分类树：出现（它是普通分类，只是固定）；不做「只给 CLI 用」的隐藏分类。
