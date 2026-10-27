# 用户信息页 + 引导页两步 + 主密码修改 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `/info` 用户信息页（运行信息 + 改用户名 + 改助记问题 + 改主密码），并把引导页改成「名字 → 必填初始主密码」两步。

**Architecture:** 后端只加搬运字节的端点（`GET /api/info`、加密笔记清单、密文替换、助记问题更新），服务端零密码学不变；改密的实质工作全在浏览器：用旧密钥解密、新密钥重加密、逐条 PUT，靠 GCM 认证标签试解判断每条笔记属于哪个密码，因此不需要迁移状态列。版本信息经 ldflags 注入，未注入时回落 `debug.ReadBuildInfo()`。

**Tech Stack:** Go 1.x `net/http` + `modernc.org/sqlite`；Vite + Svelte 5 + TypeScript + Vitest；WebCrypto（PBKDF2-HMAC-SHA256 + AES-256-GCM）。

**Spec:** `docs/superpowers/specs/2026-10-27-user-info-page-design.md` —— 本计划逐节实现该文档，冲突以文档为准。

## Global Constraints

- 时间戳统一 UTC 秒级文本 `2006-01-02T15:04:05Z`，可直接按字符串排序。
- 数据库结构变更只能新增 `sql/NNNN_*.sql`；**本计划不需要新迁移**（若某个任务觉得需要，先停下来质疑设计）。
- 状态变更类 API（PUT/POST/DELETE）必须过 CSRF 双提交；前端一律经 `web/src/lib/api.ts` 携带 `X-CSRF-Token`。
- 密码、派生密钥、明文正文不落盘、不进任何浏览器存储、不进日志；`{@html}` 只允许出现在 `web/src/components/Markdown.svelte`。
- 所有 API 响应 `Cache-Control: no-store`（`writeJSON` 已统一设置，不要绕过它）。
- 不新增任何第三方依赖（Go 与 npm 都不加）。
- 前端不引入 UI 组件库/CSS 框架/状态管理库；样式写进 `web/src/app.css`，复用现有 `.card / .stack-form / .button / .hint / .muted`。
- 每个任务收尾必须 `just test` 与 `just check` 全绿后才提交。

## Review Focus

以下六条是文档隐含、但单个任务的正常路径测试不会自动覆盖的输入。每条都在括号里标了负责它的任务，实现该任务时把测试补进去。

1. **未注入 ldflags 的构建**（Task 1）：`go run`、`just start`、`go test` 下 `GitHash/BuildDate` 为空。信息页必须显示回落值或明确的「开发构建」，不能是空白、不能 panic。
2. **引导半态**（Task 8）：名字已保存、主密码未配置时刷新，必须仍停在引导页第二步；第二步检测到「密码已配置」时必须变成解锁形态，绝不重复 POST。
3. **改密续跑不得套两层**（Task 5）：迁移中途退出后，用新密码充当旧密码续跑，已迁移的笔记必须判为 `already_done` 原样跳过，不能被再加密一次。
4. **零加密笔记时改密**（Task 5、Task 7）：清单为空时跳过旧密码验证这条路径必须能走通，`planMigration`/`runMigration` 对空数组返回全零报告而不是抛错或除零。
5. **助记问题被改成空/超长**（Task 4）：服务端必须拒绝且原问题保持不动，界面保留旧值并标红输入框，不能显示成「已清空」。
6. **过期的错误文案**（Task 4）：`master_password_exists` 的提示「主密码已设置，无法再次修改」在改密存在后成为假话，必须改成引导去信息页的说法。

---

### Task 1: 构建信息注入与 `GET /api/info`

**Files:**
- Create: `internal/build/info.go`
- Test: `internal/build/info_test.go`
- Create: `internal/web/handlers_info.go`
- Test: `internal/web/handlers_info_test.go`
- Modify: `internal/web/server.go`（注册路由）
- Modify: `internal/web/handlers_settings.go:72-87`（`application` 加 `startedAt`）
- Modify: `internal/store/store.go`（加 `SchemaVersion`）
- Modify: `justfile:50`（ldflags 注入）

**Interfaces:**
- Consumes: 无（首个任务）
- Produces:
  - `build.Info` 结构体与 `build.Describe() Info`；`build.Version/GitHash/GitDirty/BuildDate` 四个包级 `var`
  - `store.(*Store).SchemaVersion(ctx context.Context) (int, error)`
  - `GET /api/info` → JSON，字段见 Step 5
  - `application.startedAt time.Time`

- [ ] **Step 1: 写失败测试 `internal/build/info_test.go`**

覆盖两条分支，用可直接注入的变量而不是反射：

```go
func TestDescribeUsesInjectedValues(t *testing.T) {
    // 设置 GitHash="abc1234", BuildDate="2026-10-27T09:00:00Z", GitDirty="true"
    // 断言 info.GitHash/GitDate 原样、info.Injected == true、info.Version == Version
}

func TestDescribeFallsBackWhenNotInjected(t *testing.T) {
    // GitHash="", BuildDate="" → info.Injected == false
    // 且 info.GitHash 取到 debug.ReadBuildInfo 的 vcs.revision（可能为空串）
    // 断言 info.Label() 含 "（开发构建）"，且不含空括号或 "<nil>"
}
```

`Info` 需要 `Label() string`：`Version`，注入时追加 ` (abc1234, 2026-10-27T09:00:00Z)`，脏工作树再追加 `, dirty`；未注入时追加 `（开发构建）`。`Label()` 是信息页唯一展示串，两个测试都要断言它永不含空括号对。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/build/ -v`
Expected: 编译失败（`package note/internal/build` 不存在）

- [ ] **Step 3: 实现 `internal/build/info.go`**

```go
package build

var (
    Version   = "1.4.0" // 源码即当前版本，构建时可用 -X 覆盖；与应用文档版本号无关
    GitHash   = ""
    GitDirty  = ""
    BuildDate = ""
)

type Info struct {
    Version, GitHash, GitDirty, BuildDate, GoVersion string
    VcsRevision, VcsTime, VcsModified                string
    Injected                                         bool
}

func Describe() Info   // GitHash != "" && BuildDate != "" → Injected=true，否则回落 debug.ReadBuildInfo().Settings 的 vcs.revision/vcs.time/vcs.modified
func (i Info) Label() string
```

`GoVersion` 取 `runtime.Version()`。回落路径必须在没有 vcs 信息时（`go test` 单包编译）也返回安全值。

- [ ] **Step 4: 写 `internal/web/handlers_info_test.go` 的失败测试**

用现有 `newTestApp`/`httptest` 模式（见 `internal/web/server_test.go`）：

```go
func TestInfoEndpointReportsBuildAndRuntime(t *testing.T) {
    // GET /api/info → 200
    // 断言字段存在：version, go_version, goos, goarch, hostname, cpu_count,
    //   mem_alloc_bytes, mem_sys_bytes, started_at, uptime_seconds,
    //   db_path, schema_version, addr, allow_remote, build_label, git_dirty
    // 断言 started_at 匹配 ^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$
    // 断言 Cache-Control == no-store
    // 断言 schema_version == 4
}

func TestInfoEndpointHasNoSecrets(t *testing.T) {
    // 整个响应体不得含 "cipher"、"salt"、"question" 三个键（信息页不接触任何加密材料）
}
```

- [ ] **Step 5: 实现 `internal/web/handlers_info.go`**

`func (app *application) handleGetInfo(w http.ResponseWriter, r *http.Request)`，`writeJSON(w, 200, map[string]any{...})`，字段名用 Step 4 列出的 snake_case。取值来源：

| 字段 | 来源 |
| --- | --- |
| `build_label`, `git_dirty`, `version` | `build.Describe()` |
| `go_version`, `goos`, `goarch` | `runtime` |
| `hostname` | `os.Hostname()`，出错时置 `"未知"` 而非返回 500 |
| `cpu_count` | `runtime.NumCPU()` |
| `mem_alloc_bytes`, `mem_sys_bytes` | `runtime.ReadMemStats` |
| `started_at`, `uptime_seconds` | `app.startedAt` 与 `time.Since` |
| `db_path` | `app.cfg.DBPath` |
| `schema_version` | `app.store.SchemaVersion(r.Context())`，出错时返回 500 `internal` |
| `addr`, `allow_remote` | `app.cfg` |

`application` 加 `startedAt time.Time`，在 `newApplication` 里 `time.Now().UTC()`。`store.SchemaVersion` 读 `PRAGMA user_version`。路由注册 `mux.HandleFunc("GET /api/info", app.handleGetInfo)`。

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/build/ ./internal/web/ -v -run Info`
Expected: PASS

- [ ] **Step 7: 改 `justfile` 的 `build` 注入 ldflags**

Windows 走 powershell、Unix 走 bash，两份命令都要写，且 git 不可用/不在仓库内时必须退化成空串而不是让构建失败（用 `$(cmd 2>$null)` / `|| true` 兜住）。注入：

```
-X note/internal/build.GitHash=<git rev-parse --short HEAD>
-X note/internal/build.BuildDate=<date -u +%Y-%m-%dT%H:%M:%SZ>
-X note/internal/build.GitDirty=<工作树有改动则 true 否则空>
```

`Version` 不注入（源码常量就是当前版本），保持 `-s -w`。

- [ ] **Step 8: 验证注入真的生效**

Run: `just build && ./note.exe`（后台起，再 `curl -s http://127.0.0.1:8080/api/info`）
Expected: `git_dirty`/`build_label` 里能看到当前 commit 短 hash 与 UTC 构建时间；随后 `go run ./cmd/note` 起的服务里 `build_label` 含「开发构建」。

- [ ] **Step 9: `just test` + `just check` 全绿后提交**

```bash
git add internal/build internal/web internal/store/store.go justfile
git commit -m "feat(api): 新增 GET /api/info，构建信息经 ldflags 注入"
```

---

### Task 2: store 层——加密笔记清单与密文替换

**Files:**
- Modify: `internal/store/note.go`
- Test: `internal/store/note_test.go`

**Interfaces:**
- Consumes: 现有 `openTestStore(t)`、`normalizeBody`、`inTx`、`nowUTC`
- Produces:
  - `store.EncryptedCipher{ID string `json:"id"`; CipherB64 string `json:"cipher_b64"`; KDFParams string `json:"kdf_params"`}`
  - `func (s *Store) ListEncryptedCiphers(ctx context.Context) ([]EncryptedCipher, error)`
  - `func (s *Store) UpdateNoteCipher(ctx context.Context, id, cipherB64, kdfParams string) (Note, error)`
  - `store.ErrNotEncrypted`（新 `errors.New("note is not encrypted")`）

- [ ] **Step 1: 写失败测试 `TestListEncryptedCiphers`**

建 1 条明文 + 2 条加密笔记（复用 `note_test.go` 里现成的 `NoteInput{CipherB64: ...}` 写法），断言：返回 2 行、每行 `CipherB64` 非空、`KDFParams` 原样回显、结果里没有任何正文字段（结构体本身就没有 `Content` 字段，用编译期保证）。

- [ ] **Step 2: 写失败测试 `TestUpdateNoteCipherKeepsMetadata`**

```go
// 建一条加密笔记，记下 title/category_id/rev/updated_at/created_at
// 等 1 秒（或直接断言 updated_at 字符串相等即可，nowUTC 是秒级）
// UpdateNoteCipher(ctx, id, 新密文, `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":150000,"keylen":32}`)
// 断言：CipherB64 变了；Title/CategoryID/Rev/CreatedAt/UpdatedAt 与 KDFParams 之外的列全不变
// 断言：Encrypted 仍为 true
```

`rev` 与 `updated_at` 必须**不**变——这是设计文档 §4 的明确决策（改密不是用户可见的修订）。

- [ ] **Step 3: 写失败测试 `TestUpdateNoteCipherRejectsPlaintextAndMissing`**

对明文笔记调用返回 `ErrNotEncrypted`；对不存在的 id 返回 `ErrNoteNotFound`。

- [ ] **Step 4: 跑测试确认失败**

Run: `go test ./internal/store/ -run 'EncryptedCiphers|NoteCipher' -v`
Expected: 编译失败（方法未定义）

- [ ] **Step 5: 实现两个方法**

`ListEncryptedCiphers`：`SELECT id, cipher, kdf_params FROM notes WHERE is_encrypted = 1 ORDER BY created_at`，`cipher` 走 `base64.StdEncoding`。
`UpdateNoteCipher`：`inTx` 内先 `SELECT is_encrypted FROM notes WHERE id = ?`（`sql.ErrNoRows` → `ErrNoteNotFound`，为 0 → `ErrNotEncrypted`），再 `UPDATE notes SET cipher = ?, kdf_params = ? WHERE id = ?`。密文长度守卫复用 `minCipherBytes/maxCipherBytes` 常量，越界返回 `ErrInvalidNote`。同时把 `kdf_salt` 刷成 `master_password.salt`（调既有 `kdfSaltFor`），保证冗余副本与主盐一致。

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/store/ -v`
Expected: PASS（含既有测试）

- [ ] **Step 7: 提交**

```bash
git add internal/store/note.go internal/store/note_test.go
git commit -m "feat(store): 加密笔记清单与只换密文不动元数据的更新"
```

---

### Task 3: web 层——清单端点与密文替换端点

**Files:**
- Create: `internal/web/handlers_note_cipher.go`
- Test: `internal/web/note_cipher_api_test.go`
- Modify: `internal/web/server.go`（两条路由）

**Interfaces:**
- Consumes: Task 2 的 `ListEncryptedCiphers`、`UpdateNoteCipher`、`ErrNotEncrypted`
- Produces:
  - `GET /api/notes/encrypted` → `{"notes":[{id,cipher_b64,kdf_params}]}`
  - `PUT /api/notes/{id}/cipher` → 更新后的完整 `store.Note` JSON；请求体 `{cipher_b64, kdf_params}`
  - 错误码：404 `not_found`、409 `not_encrypted`、400 `validation_failed`（`fields.cipher_b64` / `fields.kdf_params`）

- [ ] **Step 1: 写失败测试**

```go
func TestListEncryptedCiphersRoute(t *testing.T) {
    // 播 1 明文 + 1 加密 → GET /api/notes/encrypted 200
    // 断言只 1 条、含 cipher_b64、不含 title 与 content
    // 关键：断言 /api/notes/encrypted 没被 /api/notes/{id} 通配吃掉
    //   （后者会对 "encrypted" 这个非 UUID 返回 400/404，两者可区分）
}

func TestUpdateNoteCipherRoute(t *testing.T) {
    // PUT 带合法 cipher_b64 + kdf_params → 200，响应 encrypted==true 且 updated_at 与建笔记时相同
    // 缺 kdf_params → 400 fields.kdf_params
    // cipher_b64 非法 base64 → 400 fields.cipher_b64
    // 对明文笔记 → 409 not_encrypted
    // 不存在的 id → 404 not_found
}

func TestCipherRoutesRequireCSRF(t *testing.T) {
    // 不带 X-CSRF-Token 的 PUT → 403 csrf_invalid，且库里密文未变
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run 'Cipher' -v`
Expected: FAIL（404，路由不存在）

- [ ] **Step 3: 实现 `handlers_note_cipher.go`**

`handleListEncryptedCiphers` 直接序列化 store 结果（空清单也要输出 `"notes":[]` 而非 `null`，前端不必判空）。
`handleUpdateNoteCipher`：`decodeJSON(w, r, &body, maxNoteBody)` → 校验（复用既有 `kdfParamsError()` 与 base64/长度检查，语义同 `validateNoteRequest` 的密文分支）→ `app.store.UpdateNoteCipher` → 按 `errors.Is` 映射 404/409/400。路由：

```go
mux.HandleFunc("GET /api/notes/encrypted", app.handleListEncryptedCiphers)
mux.HandleFunc("PUT /api/notes/{id}/cipher", app.handleUpdateNoteCipher)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/web/handlers_note_cipher.go internal/web/note_cipher_api_test.go internal/web/server.go
git commit -m "feat(api): 加密笔记清单与密文替换端点"
```

---

### Task 4: 助记问题可改

**Files:**
- Modify: `internal/store/master.go`
- Test: `internal/store/master_test.go`
- Modify: `internal/web/handlers_categories.go`（新 handler + 改 `master_password_exists` 文案）
- Test: `internal/web/server_test.go` 或新建 `internal/web/master_question_api_test.go`
- Modify: `internal/web/server.go`

**Interfaces:**
- Consumes: `MasterPassword` 结构、`ErrMasterPasswordMissing`
- Produces:
  - `func (s *Store) UpdateMasterQuestion(ctx context.Context, question string) (MasterPassword, error)`
  - `PUT /api/master-password/question` → `{configured: true, question: "..."}`
  - 错误码：409 `master_password_missing`、400 `validation_failed`（`fields.question`）

- [ ] **Step 1: 写 store 失败测试 `TestUpdateMasterQuestion`**

未配置时返回 `ErrMasterPasswordMissing` 且原值不动；配置后改成「第一份工作？」→ `GetMasterPassword` 回显新值，且 `SaltB64` 与 `CreatedAt` **不变**（改问题绝不换盐）。trim 空串与纯空白返回错误。

- [ ] **Step 2: 写 web 失败测试**

```go
func TestUpdateMasterQuestionRoute(t *testing.T) {
    // 未配置 → 409 master_password_missing
    // 配置后 PUT {question:"新问题"} → 200，GET 回来是新问题、salt_b64 与之前逐字节相同
    // PUT {question:""} → 400 fields.question，且 GET 回来仍是原问题   ← Review Focus 5
    // PUT 101 字 → 400 fields.question，原问题仍在
    // 无 CSRF 头 → 403
}

func TestMasterPasswordExistsCopy(t *testing.T) {
    // 第二次 POST /api/master-password → 409，message 不含「无法再次修改」，含「/info」或「信息页」
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/store/ ./internal/web/ -run 'MasterQuestion|ExistsCopy' -v`
Expected: 编译失败 / FAIL

- [ ] **Step 4: 实现**

`UpdateMasterQuestion`：trim 后非空且 `utf8.RuneCountInString <= 100`，`UPDATE master_password SET question = ? WHERE id = 1`，`RowsAffected==0` → `ErrMasterPasswordMissing`。
`handleUpdateMasterQuestion`：长度/空白校验放 web 层（与 `handleSetMasterPassword` 同一套文案「请填写助记问题」「助记问题请控制在 100 个字以内」）。
把 `handlers_categories.go:186` 的 `"主密码已设置，无法再次修改"` 改成 `"主密码已设置，如需更换请到信息页的主密码卡片"`（Review Focus 6）。
路由：`mux.HandleFunc("PUT /api/master-password/question", app.handleUpdateMasterQuestion)`。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/store/master.go internal/store/master_test.go internal/web
git commit -m "feat(api): 助记问题可单独修改，盐与创建时间不变"
```

---

### Task 5: 浏览器端迁移引擎 `reencrypt.ts`

**Files:**
- Create: `web/src/lib/reencrypt.ts`
- Test: `web/src/lib/reencrypt.test.ts`

**Interfaces:**
- Consumes: `crypto.ts` 的 `deriveKey/encryptNoteContent/decryptNoteContent/paramsFromNote/KDF_PARAMS/DecryptError`
- Produces（Task 7 依赖这些确切名字）:
  ```ts
  export interface EncryptedItem { id: string; cipher_b64: string; kdf_params?: string }
  export type MigrationOutcome = 'done' | 'already_done' | 'skipped_gone' | 'failed_old_key' | 'failed_network'
  export interface MigrationRow { id: string; outcome: MigrationOutcome; message?: string }
  export interface MigrationReport { rows: MigrationRow[]; done: number; skipped: number; failed: number; remaining: number }
  export interface ReencryptDeps {
    decrypt: (key: CryptoKey, id: string, cipherB64: string) => Promise<string>
    encrypt: (key: CryptoKey, id: string, plain: string) => Promise<string>
    putCipher: (id: string, cipherB64: string, kdfParams: string) => Promise<void>
    paramsJSON?: string
  }
  export async function verifyOldPassword(items: EncryptedItem[], oldKey: CryptoKey, deps: ReencryptDeps): Promise<boolean>
  export async function planMigration(items: EncryptedItem[], newKey: CryptoKey, deps: ReencryptDeps): Promise<EncryptedItem[]>
  export async function runMigration(items: EncryptedItem[], oldKey: CryptoKey, newKey: CryptoKey, deps: ReencryptDeps, onProgress?: (row: MigrationRow) => void): Promise<MigrationReport>
  ```

- [ ] **Step 1: 写失败测试**

Vitest 默认 node 环境，Node 24 自带 WebCrypto，直接用**真实** `deriveKey/encryptNoteContent`（`crypto.test.ts` 已是这种写法），只把 `putCipher` 换成内存 Map 假实现：

```ts
it('verifyOldPassword 只要清单里任意一条能解开就通过', ...)   // 旧密码只能解开第 2 条 → 仍返回 true  ← 设计文档 §5 的自审修正
it('verifyOldPassword 全部解不开时返回 false', ...)
it('planMigration 把新密钥能解开的判为已迁移并排除', ...)
it('runMigration 对已迁移的笔记不二次加密', ...)              // 断言假 putCipher 没被这些 id 调用过  ← Review Focus 3
it('runMigration 单条 putCipher 抛错时归类 failed_network 并继续跑完其余', ...)
it('putCipher 报 not_encrypted 时归为 skipped_gone', ...)     // deps.putCipher 抛 Error 且带 code
it('旧密码解不开某条时归为 failed_old_key 且不影响其余条目', ...)
it('空清单时 verifyOldPassword 返回 true、runMigration 各项计数为 0', ...)  // ← Review Focus 4
it('remaining 等于 failed_old_key 与 failed_network 的条数', ...)
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- reencrypt`
Expected: FAIL（模块不存在）

- [ ] **Step 3: 实现**

三条准则：`verifyOldPassword` 逐条试解、**第一条成功即返回 true**（不能只试第一条，也不能要求全部可解）；`planMigration` 用新密钥试解、能解开的排除；`runMigration` 先取 `planMigration` 结果，逐条 `decrypt(旧) → encrypt(新) → putCipher`，任何单条异常都归入 §6 的五种类别之一并继续。提交的 `kdf_params` 用 `deps.paramsJSON ?? JSON.stringify(KDF_PARAMS)`——设计文档要求重加密时把参数刷成当前应用参数。`report.remaining` = `failed_old_key` + `failed_network` 条数。明文只作为局部变量存在，绝不写进 `MigrationRow.message`。

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/reencrypt.ts web/src/lib/reencrypt.test.ts
git commit -m "feat(web): 主密码改密的浏览器端迁移引擎"
```

---

### Task 6: 前端 API 客户端、settings 与路由

**Files:**
- Modify: `web/src/lib/notes.ts`
- Modify: `web/src/lib/settings.ts`
- Modify: `web/src/router/router.ts`
- Test: `web/src/router/router.test.ts`、`web/src/lib/settings.test.ts`（若无则新建）

**Interfaces:**
- Consumes: Task 3、Task 4 的端点；Task 5 的 `EncryptedItem` 类型（`notes.ts` 用 `import type { EncryptedItem } from './reencrypt'`，不重复定义）
- Produces:
  - `notes.ts`: `listEncryptedCiphers(): Promise<{ notes: EncryptedItem[] }>`、`updateNoteCipher(id, cipherB64, kdfParams): Promise<void>`、`updateMasterQuestion(question): Promise<{ configured: boolean; question: string }>`
  - `settings.ts`: `SettingsState` 增加 `masterConfigured: boolean`；`loadSettings()` 并行拉两个接口
  - `router.ts`: `RouteName` 增加 `'info'`，pattern `/^\/info$/`

- [ ] **Step 1: 写 router 失败测试**

`/info` → `{name:'info',params:{}}`；`/info?x=1` 同样命中（沿用 `matchRoute` 剥查询串）；`/info/extra` → `notFound`。

- [ ] **Step 2: 写 settings 失败测试**

mock `fetch`：两个接口都 200 时 `masterConfigured` 取 `/api/master-password` 的 `configured`；`/api/master-password` 失败时整体 `status:'error'`（引导页判据不能建立在半套数据上）。

- [ ] **Step 3: 跑测试确认失败**

Run: `npm --prefix web test -- router settings`
Expected: FAIL

- [ ] **Step 4: 实现三个文件**

`updateNoteCipher` 走 `putJSON`，409 时把 `ApiRequestError.code` 透出去（Task 5 的 `skipped_gone` 判定依赖它）。`loadSettings` 用 `Promise.all` 拉 `/api/settings` 与 `/api/master-password`。

- [ ] **Step 5: 跑测试确认通过并提交**

```bash
npm --prefix web test
git add web/src/lib/notes.ts web/src/lib/settings.ts web/src/router
git commit -m "feat(web): 信息页所需客户端接口与引导完成判据"
```

---

### Task 7: `InfoView` 四张卡

**Files:**
- Create: `web/src/lib/info.ts`
- Test: `web/src/lib/info.test.ts`
- Create: `web/src/views/InfoView.svelte`
- Modify: `web/src/App.svelte`（`info` 分支 + `titles.info`）
- Modify: `web/src/components/Layout.svelte:42`（`.who` 改 `<a href="/info">`）
- Modify: `web/src/app.css:139`（`.who` 作为链接的样式）与新增信息页行样式

**Interfaces:**
- Consumes: Task 1 的 `/api/info`、Task 5 的 `reencrypt.ts`、Task 6 的三个客户端函数、`session-key.ts` 的 `deriveKey/unlockWithPassword/sessionKey`
- Produces: 路由 `/info` 的完整页面；`GET /api/info` 的读取函数 `getInfo()`（放 `web/src/lib/info.ts`）

- [ ] **Step 1: 写 `info.ts` + 纯函数测试**

`getInfo(): Promise<ServerInfo>`；`buildRows(info: ServerInfo): {label: string; value: string}[]` 断言：`build_label` 缺失时显示「未知」而不是 `undefined`；`uptime_seconds` 摊成「X 小时 Y 分」；`allow_remote` 摊成「是/否」。

- [ ] **Step 2: 实现四张卡**

1. **运行信息**：`buildRows` 的结果渲染 `<dl>`，只读。
2. **用户名**：输入框 + 保存，调 `saveName()`，服务端 `fields.name` 回显到输入框下方。
3. **主密码**：显示当前助记问题；「修改助记问题」内联表单调 `updateMasterQuestion()`，成功后更新本地显示；「修改主密码」展开改密表单（旧密码 + 新密码 + 确认 + 可选新助记问题 + 二次确认勾选「我已知晓：改密会把所有加密笔记用新密码重新加密，过程中请勿关闭页面」）。
   改密提交流程严格按设计文档 §5：`getMasterPassword()` 拿盐 → `deriveKey(旧)` → `listEncryptedCiphers()` → `verifyOldPassword()`（清单为空时直接通过）→ `deriveKey(新)` → `runMigration(..., deps 用真实 crypto + updateNoteCipher)` → `sessionKey.set(新密钥)` → 再跑一次 `planMigration` 复核 → 结果进第 4 张卡。旧密码验证失败时**不动**会话密钥、不清空已有密文。
4. **迁移状态**：`report.remaining > 0` 时出现，列出 `failed_old_key` / `failed_network` 的 id 与类别，给「继续迁移」按钮（重跑第 3 步，新密码填进旧密码栏）。

顶栏 `.who` 改成 `<a class="who" href="/info" onclick={(e) => link(e, '/info')}>`，`app.css` 给它 `text-decoration: none; color: var(--muted)` 与 hover 态，`title` 改成「用户与信息」。

- [ ] **Step 3: `just check` 与前端测试通过**

Run: `just check && npm --prefix web test`
Expected: 0 errors、PASS

- [ ] **Step 4: 提交**

```bash
git add web/src
git commit -m "feat(web): 用户信息页——运行信息、改名、改助记问题与改主密码"
```

---

### Task 8: 引导页两步式

**Files:**
- Modify: `web/src/views/OnboardingView.svelte`
- Modify: `web/src/App.svelte:31-45`（标题与门槛判据）
- Modify: `web/src/views/MasterPasswordView.svelte:80-90`（只读卡改指向 `/info`）
- Modify: `web/src/app.css:305-360`（步骤指示与第二步表单样式）
- Test: `web/src/views/onboarding.test.ts`（把两步判定抽成纯函数 `decideStep(nameSet, masterConfigured)`）

**Interfaces:**
- Consumes: Task 6 的 `settings.masterConfigured`、`notes.setMasterPassword/getMasterPassword`、`session-key.unlockKey`
- Produces: `decideStep(nameSet: boolean, masterConfigured: boolean): 0 | 1 | 2`（0 = 引导已完成，不进引导页）

- [ ] **Step 1: 写失败测试**

```ts
it('decideStep(false, false) === 1')
it('decideStep(false, true) === 1')    // 老库先设过密码但没设名字
it('decideStep(true, false) === 2')    // 名字存了密码没配 → 仍在引导，直接第二步  ← Review Focus 2
it('decideStep(true, true) === 0')     // 两步都齐 → 正常进主界面
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- onboarding`
Expected: FAIL

- [ ] **Step 3: 实现两步 UI**

步骤 1 完全沿用现有名字表单；`saveName()` 成功后只置本地 `step = 2`，**不**调 `loadSettings()`。
步骤 2 挂载时 `getMasterPassword()`：`configured === false` → 完整设置表单（密码 + 确认 + 助记问题 + 不可恢复勾选，字段与校验文案照抄 `MasterPasswordView`）；`configured === true` → 「输入主密码解锁」单输入框，只 `unlockKey`，绝不 POST（重复 POST 会 409 卡死）。 ← Review Focus 2
提交成功 → `unlockKey(password, salt)` → `loadSettings()` → `navigate('/')`。
无「跳过」按钮。标题下加「1/2 名字」「2/2 主密码」指示。

- [ ] **Step 4: 改 `App.svelte` 判据与标题**

门槛：`decideStep($settings.nameSet, $settings.masterConfigured) !== 0` 时渲染 `OnboardingView`（并把返回值当初始步骤传进去），否则渲染 `Layout`；`document.title` 的引导态用同一判定，避免出现「还在引导却显示『XX的笔记』」。

- [ ] **Step 5: 改 `MasterPasswordView` 只读卡**

「已经配置过了」卡里的「返回笔记」按钮旁改成指向 `/info` 的「去信息页修改主密码或助记问题」；保留 `?next=` 交棒逻辑不动。

- [ ] **Step 6: 全量验证并提交**

```bash
just test && just check
git add web/src
git commit -m "feat(web): 引导页改为名字+初始主密码两步，主密码必填"
```

---

### Task 9: 端到端实测

**Files:**
- Create: `.run/info-e2e.mjs`（Playwright 驱动 Edge headless，独立端口 + 独立库，沿用仓库既有 `.run/*.mjs` 约定与 `.gitignore`）

**Interfaces:**
- Consumes: 前面全部任务
- Produces: 一份可重跑的验证脚本与结论

- [ ] **Step 1: 起隔离实例**

`just build`（必须走 just 而不是裸 `go build`，否则 Task 1 注入的 ldflags 不生效，Step 3 断言的 commit 短 hash 就看不到），然后用**三个隔离参数**启动：

```
./note.exe --config .run/e2e-note.yaml --addr 127.0.0.1:8099 --db .run/e2e-info.db
```

`--config` 指向 `.run/` 下新建的 YAML 是必须的：改用户名会原子重写配置文件，不隔离就会把开发者真实的 `config/note.yaml` 里的名字改掉。三个 flag 名已核对过 `internal/config/config.go:56-59`（`config` / `addr` / `db` / `allow-remote`）。

- [ ] **Step 2: 空库引导断言**

全新库 → 步骤 1 填名字后**刷新页面** → 断言仍在引导页且是第二步形态 → 设密码进主界面 → 断言 `master_password` 表只有一行、`salt` 与首次设置一致 → 再演「POST 成功后立刻刷新」→ 断言第二步是解锁形态、Network 里无 `POST /api/master-password`。

- [ ] **Step 3: 信息页与改密断言**

建两条加密笔记 → `/info` 断言 `build_label` 含 commit 短 hash → 改用户名并刷新确认持久 → 改助记问题并确认盐未变（比对 `GET /api/master-password` 的 `salt_b64`）→ 用错误旧密码改密，断言被拦下且两条 `cipher` 字节不变 → 正确旧密码跑完 → 刷新后用新密码解锁两条 → **中途退出**：只迁移一条就 reload，用新密码填旧密码栏续跑，断言 `remaining === 0` 且两条都能用新密码解开。

- [ ] **Step 4: 明文残留扫描**

沿用 CHANGELOG 里已验证的扫描法：改密前后 dump 数据库文件，断言新密码明文与解密出的正文都不在文件里。

- [ ] **Step 5: 记录结论**

`just test` + `just check` + 本脚本全绿后，把实现过程与关键数据流写进 `CHANGELOG.md` 的 `[Unreleased]`（Added / Changed / Security），然后提交。

---

### Task 10: 文档同步

**Files:**
- Modify: `docs/PRD.md`
- Modify: `docs/TECH_DESIGN.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: 设计文档 §9 的同步清单
- Produces: 文档与实现一致

- [ ] **Step 1: PRD v1.3 → v1.4**

2.2 非目标移除「修改主密码」；5.3.1 第 4 条改写为「可改密、不可找回」；新增信息页与改密流程的 FR；引导页章节补两步式与半态解锁的 FR/AC；3.x 页面清单「9 个主视图」改 10 个；Q5 结论更新；§14 候选项移除「修改密码」。

- [ ] **Step 2: TECH_DESIGN**

1.3 非目标同步；1.2 安全不变量加「主密码可改仍不可找回，旧密码是否正确只由浏览器试解判定」；§6 补改密与重加密流程；§1102 口令遗忘行补「改密需旧密码」；接口表加四个新端点；前端启动流程改写引导页判定；目录结构加 `internal/build/`；构建章节同步 `justfile` 的新 ldflags。

- [ ] **Step 3: AGENTS.md**

硬性约定补：主密码可改不可找回；`GET /api/info` 与其他 API 一样 `no-store`；`PUT /api/notes/{id}/cipher` 不改 `updated_at/rev` 属于有意设计。

- [ ] **Step 4: 自查引用后提交**

逐条核对本计划与文档里出现的行号、常量名（`masterSaltBytes`、`maxNoteBody`、`kdfParamsError`）与实际代码一致，避免文档里的指针失效。

```bash
git add docs AGENTS.md
git commit -m "docs: 同步 PRD/技术方案到 v1.4，主密码改为可改不可找回"
```
