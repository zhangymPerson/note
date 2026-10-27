# 笔记 CRUD + 分类 + 全局主密码加密 + Markdown 预览 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现笔记新增/查看/编辑/删除/列表分页搜索、可管理的分类体系、全局主密码的浏览器端 AES-GCM 加密笔记、以及 marked+DOMPurify 的 Markdown 编写预览。

**Architecture:** 服务端只做字节存储——加密笔记的密文对后端是黑盒，密钥派生与加解密全部在浏览器 WebCrypto 完成，密码永不发往服务端；密文格式自描述（PBKDF2 参数 + AAD 绑定笔记 ID），未来 CLI 可按 spec 第 2 节直接互通。分类与主密码配置是两张新表 + notes 一个新列。

**Tech Stack:** Go 1.26 标准库 net/http、modernc.org/sqlite、Svelte 5 runes、WebCrypto（浏览器与 Node 22+ 内置）、marked、dompurify。

**Spec:** `docs/superpowers/specs/2026-10-27-note-crud-category-encryption-design.md`

## Global Constraints

- CGO_ENABLED=0；不引入 UI 组件库/CSS 框架/状态管理库；`marked`、`dompurify` 为 spec 批准的功能性例外，仅此两个新运行时依赖。
- 时间戳一律 UTC 秒级文本 `2006-01-02T15:04:05Z`（store 包常量 `timeLayoutUTC`）。
- 表结构变更只走新增 `sql/NNNN_*.sql` 迁移；禁止手改既有迁移。
- 状态变更 API（POST/PUT/DELETE）经现有 `csrfProtect`；前端经 `web/src/lib/api.ts` 统一携带 `X-CSRF-Token`。
- `{@html}` 仅允许出现在 `web/src/components/Markdown.svelte`，输入必须先过 `DOMPurify.sanitize`。
- 正文 ≤ 2000 个 Unicode 码点（Go `utf8.RuneCountInString`，JS `Array.from(s).length`）；标题去首尾空白后 1–200 码点。
- 密码、明文正文、派生密钥不写日志/localStorage/sessionStorage/IndexedDB。
- 每个 Task 结束先 `just test` + `just check` 通过再 commit。

## Review Focus

以下是 spec 未逐条写出、但成品最容易翻车的输入，各 Task 已按行落测试：

- 正文含 emoji/生僻字（码点 > U+FFFF）时，Go 与 JS 的 2000 上限必须给出一致的通过/拒绝判定 → Task 4/10。
- 错误密码、被篡改密文、`kdf_params.iter` 越界三类失败在前端必须收敛为同一句「密码错误或数据损坏」，不泄露原因 → Task 7。
- `cipher_b64` 非法 base64、解码后 < 29 字节（nonce12+tag16 下限）、> 64 KiB 三种畸形输入，后端 400 拒绝且 panic-free → Task 5。
- 锁定/离开详情页后，已解密正文不得残留在任何组件状态或列表缓存里 → Task 12。
- 分类改名大小写冲突（`Work` vs `work`，UNIQUE COLLATE NOCASE）返回 409 而非 500 → Task 2/6。

---

### Task 1: 迁移 0004（categories / master_password / notes.category_id）

**Files:**
- Create: `sql/0004_note_app.sql`
- Modify: `internal/store/store_test.go`（版本号断言 3 → 4）

**Interfaces:**
- Produces: 表 `categories(id INTEGER PK AUTOINCREMENT, name TEXT NOT NULL UNIQUE COLLATE NOCASE, created_at TEXT NOT NULL)`（含预置行 id=1「未分类」）、单行表 `master_password(id INTEGER PK CHECK(id=1), salt BLOB NOT NULL, question TEXT NOT NULL, created_at TEXT NOT NULL)`、列 `notes.category_id INTEGER`（NULL=未分类）。

- [ ] **Step 1: 写失败测试** — `store_test.go` 两个 Open 测试的 `user_version` 期望改为 4；新增 `TestMigration0004Objects`：打开临时库后断言 `categories` 首行为 `(1,'未分类')`、`INSERT INTO categories(name,created_at) VALUES('未分类',…)` 因 UNIQUE 失败、`notes` 表存在 `category_id` 列（`PRAGMA table_info`）、`INSERT INTO master_password` 第二行因 `CHECK(id=1)` 失败。
- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/store -run 'TestOpen|TestMigration0004' -v`，预期版本断言失败。
- [ ] **Step 3: 写迁移** — `sql/0004_note_app.sql`：按 Interfaces 建两张表 + `ALTER TABLE notes ADD COLUMN category_id INTEGER REFERENCES categories(id);`，预置行用 `INSERT INTO categories (id, name, created_at) VALUES (1, '未分类', '2000-01-01T00:00:00Z');`。文件头注释说明默认分类守卫在应用层。
- [ ] **Step 4: 跑全部 store 测试确认通过** — `go test ./internal/store -v`。
- [ ] **Step 5: Commit** — `git commit -m "feat(db): add categories, master_password tables and notes.category_id"`

### Task 2: store 分类层

**Files:**
- Create: `internal/store/category.go`
- Test: `internal/store/category_test.go`

**Interfaces:**
- Consumes: Task 1 的表。
- Produces:
```go
type Category struct { ID int64 `json:"id"`; Name string `json:"name"`; NoteCount int `json:"note_count"` }
var ErrCategoryExists / ErrReservedCategory / ErrNotFound            // sentinel errors
type ErrCategoryNotEmpty struct { Count int }                        // Error(): "分类下仍有 N 条笔记"
func (s *Store) ListCategories(ctx) ([]Category, error)              // 按 id 升序，含 note_count（LEFT JOIN notes）
func (s *Store) CreateCategory(ctx, name string) (Category, error)   // name 已去空白；1–30 码点校验在 web 层；NOCASE 冲突→ErrCategoryExists
func (s *Store) RenameCategory(ctx, id int64, name string) error     // id==1→ErrReservedCategory；不存在→ErrNotFound
func (s *Store) DeleteCategory(ctx, id int64) error                  // id==1→ErrReservedCategory；引用数>0→ErrCategoryNotEmpty；同事务内先把该分类笔记的 category_id 置 NULL 再删（防竞态兜底）
```

- [ ] **Step 1: 写失败测试** — 建库后：Create 两个分类 → List 返回 3 项且 note_count 全 0；重复名 `'work'` 后再 `'WORK'` → ErrCategoryExists；Rename/Delete id=1 → ErrReservedCategory；插入一条 `category_id=2` 的笔记后 Delete 2 → `errors.As` 得 `ErrCategoryNotEmpty{Count:1}`；把笔记改到分类 3 后 Delete 2 成功且 List 剩 2 项。测试内直接用 `s.db.Exec` 插笔记（Task 4 前没有 CreateNote）。
- [ ] **Step 2: 确认失败** — `go test ./internal/store -run TestCategor -v`。
- [ ] **Step 3: 实现 category.go** — SQL 用 `LIKE ? ESCAPE` 无关；NOCASE 唯一冲突判定：`errors.Is(err, modernc 唯一约束)` 不稳，改为插入前先 `SELECT 1 FROM categories WHERE name = ?`（NOCASE 列比较天然忽略大小写）+ 捕获唯一错误双保险。
- [ ] **Step 4: 确认通过** — `go test ./internal/store -v`。
- [ ] **Step 5: Commit** — `git commit -m "feat(store): category CRUD with reserved default and in-use guard"`

### Task 3: store 主密码配置层

**Files:**
- Create: `internal/store/master.go`
- Test: `internal/store/master_test.go`

**Interfaces:**
- Produces:
```go
type MasterPassword struct { SaltB64 string `json:"salt_b64"`; Question string `json:"question"`; CreatedAt string `json:"created_at"` }
var ErrMasterPasswordExists
func (s *Store) GetMasterPassword(ctx) (MasterPassword, bool, error)   // 未配置 → zero,false,nil
func (s *Store) SetMasterPassword(ctx, salt []byte, question string) (MasterPassword, error) // 已存在→ErrMasterPasswordExists；salt 必须 16 字节否则报错
```

- [ ] **Step 1: 写失败测试** — 初始 Get → `(_, false, nil)`；Set(16B, "你的生日是多少？") 后 Get 回读 SaltB64 为同字节 base64.StdEncoding、Question 一致；二次 Set → ErrMasterPasswordExists；Set(15 字节) → error。
- [ ] **Step 2: 确认失败** — `go test ./internal/store -run TestMaster -v`。
- [ ] **Step 3: 实现 master.go** — `INSERT INTO master_password (id, salt, question, created_at) VALUES (1, ?, ?, ?)`。
- [ ] **Step 4: 确认通过** — `go test ./internal/store -v`。
- [ ] **Step 5: Commit** — `git commit -m "feat(store): one-time master password salt and question"`

### Task 4: store 笔记层（CRUD + 列表搜索分页）

**Files:**
- Create: `internal/store/note.go`
- Test: `internal/store/note_test.go`

**Interfaces:**
- Consumes: Task 1 列、Task 2 分类、`RecordSearch`（activity.go）。
- Produces:
```go
type Note struct { // json tag 蛇形：id,title,encrypted,category_id,kind,remark,rev,content,cipher_b64,kdf_params,created_at,updated_at
  ID string; Title string; Content *string; Encrypted bool; Cipher []byte; KDFParams *string
  CategoryID *int64; Kind string; Remark string; Rev int; CreatedAt, UpdatedAt string }
type NoteInput struct { Title string; Content *string; CipherB64 string; KDFParams string; CategoryID *int64 }
  // 明文态：Content 非 nil 且 CipherB64==""；加密态反之。结构校验在 web 层，本层再兜底
var ErrNoteNotFound
func NewNoteID() string                                   // crypto/rand 手搓 UUIDv4（8-4-4-4-12，version/variant 位正确），不加依赖
func (s *Store) CreateNote(ctx, in NoteInput) (Note, error)
func (s *Store) GetNote(ctx, id string) (Note, error)
func (s *Store) UpdateNote(ctx, id string, in NoteInput) (Note, error) // rev+1, updated_at 刷新；不存在→ErrNoteNotFound
func (s *Store) DeleteNote(ctx, id string) error                       // 0 行→ErrNoteNotFound；提交后执行 PRAGMA wal_checkpoint(TRUNCATE)
type NoteListItem struct { ID,Title string; Encrypted bool; Content *string; CategoryID *int64; UpdatedAt string; CategoryName string } // Content 仅明文笔记前 140 码点摘要
type NotePage struct { Items []NoteListItem `json:"items"`; Total int `json:"total"`; Page int `json:"page"`; PerPage int `json:"per_page"` }
func (s *Store) ListNotes(ctx, q string, categoryID int64, page int) (NotePage, error)
  // categoryID==0 全部；page 从 1 起，PerPage=20；q 非空时先 RecordSearch、标题命中排前（同 activity.go 转义法）、加密笔记只匹配标题（content IS NULL 天然满足）；空 q 时排除加密笔记的摘要列
```

- [ ] **Step 1: 写失败测试** — 覆盖：CreateNote 明文（kind 默认 'note'，rev=1，Content 回读）；CreateNote 加密态（CipherB64 合法、Content 落库 NULL、GetNote 回读 Cipher 字节相等）；明文转加密 Update（同 id 先明文后传 CipherB64+空 Content → Encrypted=true 且 Content nil；反向被 0001 CHECK 拒 → error）；Update rev 递增；Delete 后 Get→ErrNoteNotFound；ListNotes：插 45 条 → page1 20 条、page3 5 条、Total 45、updated_at 倒序；q 命中标题的排在前；categoryID 过滤正确；emoji 正文 2000 码点边界（1999 过 / 2001 拒由 web 层，本层只测存取 2000+emoji 不截断）；NewNoteID 格式正则 + 两次不同。
- [ ] **Step 2: 确认失败** — `go test ./internal/store -run TestNote -v`。
- [ ] **Step 3: 实现 note.go** — 摘要用 `[]rune` 切片取前 140；`cipher_b64` 仅在需要时 base64.StdEncoding 编解码；ListNotes 的 COUNT 与分页共用 WHERE（拼参数化 SQL，category 条件 `category_id = ?`，NULL 分类筛选本期不做）。
- [ ] **Step 4: 确认通过** — `go test ./internal/store -v`。
- [ ] **Step 5: Commit** — `git commit -m "feat(store): notes CRUD with paged search and category filter"`

### Task 5: web 笔记 API

**Files:**
- Create: `internal/web/handlers_notes.go`
- Modify: `internal/web/server.go`（注册路由，移除 `GET /api/notes` 旧行）、`internal/web/handlers_stats.go`（删除 handleSearchNotes，保留 handleStats）
- Test: `internal/web/notes_api_test.go`

**Interfaces:**
- Consumes: Task 4 store 方法、`decodeJSON`（errors.go）、现有错误码约定（`writeError(w, code, ...)`）。
- Produces: 路由 `GET /api/notes?q=&page=&category=`、`POST /api/notes`、`GET/PUT/DELETE /api/notes/{id}`；请求体 `{title, content?, cipher_b64?, kdf_params?, category_id?}`；响应 Note JSON；错误码 `validation_failed`(400+fields)、`not_found`(404)、`body_invalid`(400)。

- [ ] **Step 1: 写失败测试** — httptest 走新 handler 链（复用 server_test.go 的 newTestHandler 风格，需带 store）：创建明文 201 且响应含 id；空标题/201 码点标题/2001 码点正文/emoji 2001 → 400 fields 指明字段；加密态缺 kdf_params 或非法 base64 或解码后 28 字节或 >64KiB → 400；GET 列表 page=0 → 按 1 处理；GET 不存在的 id → 404；PUT 改标题 rev+1；DELETE → 204 空体，再 GET 404；POST 无 CSRF 头 → 403（csrfProtect 已有）。
- [ ] **Step 2: 确认失败** — `go test ./internal/web -run TestNotesAPI -v`。
- [ ] **Step 3: 实现 handlers_notes.go** — `validateNoteInput(in) map[string]string`：标题 `strings.TrimSpace` 后 1–200 码点；明文 content 存在时 ≤2000 码点（不 trim）；加密态要求 cipher_b64 可解码且 29≤len≤65536、kdf_params 为合法 JSON 且 `algo=="pbkdf2"`、`iter∈[100000,1000000]`、`keylen==32`；category_id 非空时查存在性（不存在→400）。decodeJSON 上限沿用 4096 需放宽：笔记体走新常量 `maxNoteBody = 128<<10`。
- [ ] **Step 4: 确认通过** — `go test ./internal/web -v`（server_test.go 里原 `/api/notes?q=` 相关断言按新响应形状 `{items,total,page,per_page}` 更新）。
- [ ] **Step 5: Commit** — `git commit -m "feat(api): notes CRUD endpoints with validation and paging"`

### Task 6: web 分类与主密码 API

**Files:**
- Create: `internal/web/handlers_categories.go`、`internal/web/handlers_master.go`
- Modify: `internal/web/server.go`（注册路由）
- Test: `internal/web/cats_master_api_test.go`

**Interfaces:**
- Produces: `GET /api/categories` → `{items:[Category]}`；`POST /api/categories {name}` → 201；`PUT /api/categories/{id} {name}` → 200；`DELETE /api/categories/{id}` → 204；`GET /api/master-password` → `{configured, salt_b64, question}`（未配置时后两字段空串）；`POST /api/master-password {salt_b64, question}` → 201。错误映射：`ErrCategoryExists`/`ErrMasterPasswordExists`→409 `already_exists`；`ErrReservedCategory`→409 `reserved_category`；`ErrCategoryNotEmpty`→409 `category_not_found_or_in_use` 改为 `category_in_use`（message 含笔记数）；`ErrNotFound`→404；名称 1–30 码点、question 1–100 码点、salt_b64 解码必须 16 字节 → 400。

- [ ] **Step 1: 写失败测试** — 分类：初始 GET 只含未分类；POST "工作" 201；再 POST "工作" → 409；POST "WORK" 后 PUT 该 id 为 "work" → 409（NOCASE）；PUT/DELETE id=1 → 409 reserved；建笔记挂分类后 DELETE → 409 且 message 含 "1"；DELETE 空分类 → 204。主密码：GET configured=false；POST 合法 201；二次 POST → 409；POST 非法 salt（10 字节 b64）→ 400。
- [ ] **Step 2: 确认失败** — `go test ./internal/web -run 'TestCategor|TestMaster' -v`。
- [ ] **Step 3: 实现两个 handler 文件并注册路由**。
- [ ] **Step 4: 确认通过** — `go test ./... -v`。
- [ ] **Step 5: Commit** — `git commit -m "feat(api): category management and one-time master password endpoints"`

### Task 7: 前端 crypto.ts + session-key.ts

**Files:**
- Create: `web/src/lib/crypto.ts`、`web/src/lib/session-key.ts`
- Test: `web/src/lib/crypto.test.ts`

**Interfaces:**
- Consumes: Node 24 内置 `globalThis.crypto.subtle`（vitest 直接可用）。
- Produces:
```ts
export interface KdfParams { algo:'pbkdf2'; prf:'HMAC-SHA256'; iter:number; keylen:number }
export const KDF_PARAMS: KdfParams // {algo:'pbkdf2',prf:'HMAC-SHA256',iter:150000,keylen:32}
export class DecryptError extends Error // message 固定「密码错误或数据损坏」
export function normalizePassword(p:string): string          // p.normalize('NFC')
export function generateSaltB64(): string                    // 16B crypto.getRandomValues → b64
export function bytesToB64(b:Uint8Array): string; export function b64ToBytes(s:string): Uint8Array
export async function deriveKey(password:string, saltB64:string, params:KdfParams): Promise<CryptoKey>
  // iter 越界（<100000 或 >1000000）或 keylen!==32 → throw DecryptError
export async function encryptNoteContent(key:CryptoKey, noteId:string, content:string): Promise<string> // 返回 nonce||ct||tag 的 b64
export async function decryptNoteContent(key:CryptoKey, noteId:string, cipherB64:string): Promise<string> // 任何失败 → DecryptError
export const sessionKey: { current(): CryptoKey|null; set(k:CryptoKey|null):void } // 模块级内存变量，非 store
export const hasKey: readable<boolean>   // 内部 writable 镜像，set() 时同步；供 Layout 锁定按钮显隐
```
AAD 固定 `new TextEncoder().encode('note:v1:' + noteId)`。

- [ ] **Step 1: 写失败测试** — 往返：deriveKey('密码pàss123…', salt) → encrypt → decrypt 等于原文（含 emoji/换行）；错误密码 decrypt → 抛 DecryptError 且 message 精确等于「密码错误或数据损坏」；把密文 b64 中间一个字符翻转 → 同错误；用另一 noteId 解密同密文（AAD 移植）→ 同错误；deriveKey iter=99999 → DecryptError；generateSaltB64 长度 16 字节且两次不同。
- [ ] **Step 2: 确认失败** — `npm --prefix web exec -- vitest run src/lib/crypto.test.ts`。
- [ ] **Step 3: 实现两个文件** — PBKDF2 派生：`importKey('raw', enc(password), 'PBKDF2', false, ['deriveKey'])` → `deriveKey({name:'PBKDF2', salt, iterations, hash:'SHA-256'}, ..., {name:'AES-GCM', length:256}, false, ['encrypt','decrypt'])`。
- [ ] **Step 4: 确认通过** — 同上命令。
- [ ] **Step 5: Commit** — `git commit -m "feat(web): WebCrypto PBKDF2+AES-GCM note encryption and in-memory key"`

### Task 8: Markdown 渲染管道

**Files:**
- Create: `web/src/lib/markdown.ts`、`web/src/components/Markdown.svelte`
- Modify: `web/package.json`（`npm --prefix web i marked dompurify` 加 dependencies；`npm --prefix web i -D jsdom` 仅测试环境用）
- Test: `web/src/lib/markdown.test.ts`（文件头 `// @vitest-environment jsdom`）

**Interfaces:**
- Produces:
```ts
export function renderMarkdown(source: string): string // marked.parse(source,{async:false,gfm:true}) → DOMPurify.sanitize(html,{ADD_ATTR:['target']}) → 输出
```
`Markdown.svelte`：`let { source }: { source: string } = $props()`；`<div class="md">{@html renderMarkdown(source)}</div>`（全仓唯一 `{@html}`）。

- [ ] **Step 1: 安装依赖** — `npm --prefix web i marked dompurify && npm --prefix web i -D jsdom`（锁文件随之更新，属功能依赖非 CSS/UI 库，符合 Global Constraints 例外）。
- [ ] **Step 2: 写失败测试** — 渲染 `# 标题` 与 `**粗体**` 含对应标签；换行保留由 pre 样式负责不测；XSS 载荷：`<img src=x onerror=alert(1)>` 输出无 `onerror`、`<script>` 被剥、`[x](javascript:alert(1))` 无 `javascript:`、`<iframe>` 被剥；代码块内 markdown 源码不被二次解析。
- [ ] **Step 3: 确认失败** — `npm --prefix web exec -- vitest run src/lib/markdown.test.ts`。
- [ ] **Step 4: 实现 markdown.ts + Markdown.svelte** — dompurify 在 jsdom 环境用 `DOMPurify(window)` 显式注入：`import DOMPurify from 'dompurify'`，`const purifier = DOMPurify.sanitize` 直接调用即可（v3 自动检测全局 window；若测试不通过则改为 `DOMPurify(new DOMWindow())` 封装，实现者按实际 API 二选一，保持导出签名不变）。
- [ ] **Step 5: 确认通过 + 全量前端测试** — `npm --prefix web test`。
- [ ] **Step 6: Commit** — `git commit -m "feat(web): sanitized markdown render pipeline with marked and dompurify"`

### Task 9: 前端 API 客户端（api.ts 扩展 + notes.ts）

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/lib/notes.ts`
- Test: `web/src/lib/notes.test.ts`

**Interfaces:**
- Consumes: 后端 Task 5/6 的 JSON 形状。
- Produces:
```ts
// api.ts 新增（putJSON 重构为共用 sendJSON(method, path, payload?)，保留 403 刷新重试）：
export async function postJSON<T>(path:string, payload:unknown): Promise<T>
export async function deleteJSON(path:string): Promise<void>   // 204 无体
// notes.ts：
export interface NotePayload { title:string; content?:string|null; cipher_b64?:string; kdf_params?:string; category_id?:number|null }
export interface NoteMeta { id:string; title:string; encrypted:boolean; category_id:number|null; kind:string; remark:string; rev:number; content:string|null; cipher_b64?:string; kdf_params?:string; created_at:string; updated_at:string }
export interface Category { id:number; name:string; note_count:number }
listNotes(q:string, category:number, page:number): Promise<{items:NoteMeta[]; total:number; page:number; per_page:number}>
createNote/getNote/updateNote/deleteNote；listCategories/createCategory/renameCategory/deleteCategory
getMasterPassword(): Promise<{configured:boolean; salt_b64:string; question:string}>; setMasterPassword(saltB64, question)
```

- [ ] **Step 1: 写失败测试** — mock `globalThis.fetch`：listNotes 拼 query `?q=&page=&category=`（空值省略）；createNote POST 带 `X-CSRF-Token`；403 后刷新 token 重试一次（沿用 putJSON 测试模式）；deleteJSON 204 返回 undefined 不解析 body；错误响应 `{error:{code:'category_in_use',message}}` → ApiRequestError.code 透传。
- [ ] **Step 2: 确认失败** — vitest 单文件。
- [ ] **Step 3: 实现**。
- [ ] **Step 4: 确认通过** — `npm --prefix web test`。
- [ ] **Step 5: Commit** — `git commit -m "feat(web): notes and categories API client with POST/DELETE helpers"`

### Task 10: 编辑器视图（Markdown 预览 + 分类 + 加密勾选）

**Files:**
- Rewrite: `web/src/views/NoteEditorView.svelte`（现为占位）
- Modify: `web/src/app.css`（编辑器/预览/计数样式）、`web/src/App.svelte`（titles 增删）
- Test: 手工（视图层不进 vitest，逻辑已在 lib 层测）

**Interfaces:**
- Consumes: Task 7/8/9 全部导出；`settings` store；路由 `edit`/`new` 已有。

- [ ] **Step 1: 实现编辑器** — 字段：标题 input、分类 select（listCategories 填充，首项「未分类」=null）、textarea 正文 + 右侧/下方 `Markdown.svelte` 实时预览（`$derived` + 300ms 防抖）、码点计数 `Array.from(text).length`/2000 超限红字并禁用保存、加密勾选框。
- [ ] **Step 2: 加密提交流程** — 勾选加密时：`getMasterPassword()` 未配置 → `navigate('/master-password?next=' + 当前编辑内容存内存草稿对象)`；已配置且 `sessionKey.current()` 为空 → 表单内联主密码输入框，提交 `deriveKey` 成功即 `sessionKey.set`；正文 `encryptNoteContent(key, id??NewID占位, content)`——新建笔记 AAD 需要 id：由后端 POST 前无法预知行——**改为客户端先生成 UUID**：notes.ts 增加 `newClientNoteID()`（crypto.randomUUID），POST 请求体加 `id` 字段，后端 CreateNote 接受可选 id（缺失才服务端生成；非法格式 400）。⚠️ 此契约改动并入本 Task：Task 5 的 CreateNote/handler 支持 `id` 字段（测试补一条：带合法 id 创建后 GET 该 id 成功）。
- [ ] **Step 3: 编辑加密笔记** — 打开时 `cipher_b64` 存在 → 先要求解锁（同详情页组件逻辑可复用函数 `unlockWithPassword` 放 session-key.ts：derive + 试解密当前笔记 + set），解密后的明文进 textarea；保存用新 nonce 重新加密，PUT 只发新 cipher。
- [ ] **Step 4: 校验与错误回显** — 400 fields 映射到字段下方；密码字段永不回显；离开未保存确认（`beforeunload`，FR-1.8）。
- [ ] **Step 5: `just check` 通过 + Commit** — `git commit -m "feat(web): markdown editor with live preview, category select and client-side encryption"`

### Task 11: 列表视图 + 首页搜索对接新 API

**Files:**
- Rewrite: `web/src/views/NoteListView.svelte`
- Modify: `web/src/views/HomeView.svelte`（search 结果改用 listNotes 响应形状）、`web/src/components/Layout.svelte`（导航加「分类」）、`web/src/app.css`

- [ ] **Step 1: 列表页** — 分类筛选 chips（全部 + 各分类，来自 listCategories）、搜索框（回车提交、q 同步 URL `?q=&category=&page=`，matchRoute 已忽略 query，用 `location.search` 读写）、行：标题 /「已加密」徽标 / 明文摘要（pre-wrap 文本插值，禁 markdown 渲染）/ 更新时间 / 删除按钮（confirm 含「永久删除」文案，加密笔记删除不要求密码）。
- [ ] **Step 2: 分页** — 上一页/下一页 + `第 x/y 页`，total 换算页数；翻页保留 q 与 category。
- [ ] **Step 3: HomeView 适配** — 现有搜索命中 `{items}` 形状变化处更新；命中行点击跳详情。
- [ ] **Step 4: `just test` 全绿 + Commit** — `git commit -m "feat(web): note list with category filter, paging and delete; home search adapts"`

### Task 12: 详情/解锁/忘记密码 + 主密码设置页 + 分类管理页 + 路由

**Files:**
- Rewrite: `web/src/views/NoteDetailView.svelte`、`web/src/views/EncryptView.svelte`→删除、`web/src/views/ForgotPasswordView.svelte`
- Create: `web/src/views/MasterPasswordView.svelte`、`web/src/views/CategoryManagerView.svelte`
- Modify: `web/src/router/router.ts`（RouteName 增 `'master'|'categories'`，patterns 增 `/master-password`、`/categories`；删 `encrypt` 路由）、`web/src/App.svelte`（路由渲染分支与 titles；`encrypt` 分支移除）、`web/src/components/Layout.svelte`（`sessionKey` 有值时显示「锁定」按钮）

- [ ] **Step 1: 详情页** — 明文：Markdown.svelte 渲染正文；加密未解锁：密码框 + 折叠「查看密码提示」（GET master-password 的 question）+「忘记密码？」链接；解锁成功缓存密钥（同页与编辑器共用，刷新失效）；操作区：编辑、删除（二次确认）、锁定（`sessionKey.set(null)` + 清空本页正文字段——Review Focus 第 4 条）。
- [ ] **Step 2: 忘记密码页** — 展示问题、无找回声明、仅「返回」与「删除笔记」（204 后 navigate('/notes')）。
- [ ] **Step 3: 主密码设置页** — 密码/确认（不一致 400 前端拦截）、助记问题、不可恢复警告 + 必选确认框；提交 `generateSaltB64` + `setMasterPassword`；成功后若带 next 流程回编辑器继续加密；已配置时页面只读展示问题并提示「只能配置一次」。
- [ ] **Step 4: 分类管理页** — 列表+计数、新增、改名（id=1 行无改名/删除按钮）、删除走 confirm，409 `category_in_use` 时把 message（含笔记数）显示在行内。
- [ ] **Step 5: 顶栏锁定按钮** — Layout 订阅 sessionKey 状态（session-key.ts 导出 `hasKey: readable<boolean>` 包装）；点击 set(null) + toast「已锁定」。
- [ ] **Step 6: `just test`+`just check` + Commit** — `git commit -m "feat(web): detail unlock, forgot-password, master setup and category manager views"`

### Task 13: 端到端验证 + 文档同步

**Files:**
- Modify: `docs/PRD.md`（v1.3：偏离表 6 项 + Markdown 移出非目标 + AC 增删）、`docs/TECH_DESIGN.md`（8.4/8.5 替换为 spec 第 2/5 节，1.2 不变量修订）、`AGENTS.md`（`{@html}` 例外表述）、`CHANGELOG.md`

- [ ] **Step 1: Playwright 全链路** — Edge 无头脚本（沿用 `.run/verify.mjs` 模式）：设主密码 → 新建 Markdown 笔记勾选加密 → 数据库断言 content NULL/cipher 非空 → 刷新 → 错误密码统一提示 → 正确密码解锁 → 编辑保存 nonce 变化 → 锁定 → 分类增改删全周期（含 409 用例界面文案）→ 截图三主题下详情页。
- [ ] **Step 2: 跨客户端互通证明（CLI 预演）** — Node 脚本读 sqlite 文件（`node:sqlite` 或临时用后端 API 取 cipher_b64+salt），用与 crypto.ts 相同参数解密同一笔记，输出与原文一致——证明 spec 第 2 节格式对非浏览器客户端成立。
- [ ] **Step 3: 明文残留检查** — 按 PRD 9.2 方法：标记文本 → 加密 → `grep -a` 主库与 WAL 无命中。
- [ ] **Step 4: 文档改写 + `just test`/`just check` 全绿 + CHANGELOG** — 按 AGENTS.md 规则记录实现过程与关键决策。
- [ ] **Step 5: Commit** — `git commit -m "docs: sync PRD v1.3 and tech design for client-side encryption; e2e verified"`
