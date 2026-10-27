# 设计：用户信息页（运行信息 + 改名 + 改助记问题 + 改主密码）

日期：2026-10-27 · 状态：待评审 · 覆盖 PRD v1.3 的 2.2 / 5.3.1 / Q5 / §14 中「修改主密码属非目标」的结论，PRD 升至 v1.4

## 1. 需求与对现有文档的偏离

| 项 | 本设计 | PRD v1.3 / TECH_DESIGN 原案 | 偏离原因 |
| --- | --- | --- | --- |
| 修改主密码 | 支持：旧密码验证 + 浏览器端批量重加密全部加密笔记 | 明确列为非目标（PRD 2.2 第 72 行、5.3.1 第 4 条、Q5；TECH_DESIGN 1.3 第 37 行、§1102） | 用户明确要求 |
| 密码找回 | 仍然不提供，任何形式都没有 | 不提供 | 不变。改密与找回是两件事：改密要求知道旧密码，服务端始终没有验证口令的能力 |
| 助记问题 | 可随时单独修改，不要求旧密码 | 只在首次设置时写入 | 问题本就是明文存储、无解密价值 |
| 用户名 | 在信息页修改（复用已有 `PUT /api/settings/name`） | 仅引导页设置 | 用户要求 |
| 运行信息 | 新增 `GET /api/info`：版本、git hash、构建日期、OS 与进程运行态 | 无此概念，二进制也没有版本注入 | 用户要求 |

OS 信息粒度取「标准库能给的运行时信息」，不引入 `golang.org/x/sys` 之类的依赖去读注册表拿「Windows 10 Pro 19045」这种产品名。

## 2. 入口与页面结构

- `web/src/components/Layout.svelte:42`：`.who` 由 `<span>` 改为 `<a class="who" href="/info">`，沿用同一份 `link(event, '/info')`，保留中键/右键新标签打开的既有行为。
- `web/src/router/router.ts`：`RouteName` 增加 `'info'`，`patterns` 增加 `{ name: 'info', pattern: /^\/info$/ }`；`App.svelte` 渲染新视图 `web/src/views/InfoView.svelte`。
- 页面四张卡，全部复用现有 `.card / .stack-form / .button / .hint` 类，不引入组件库或 CSS 框架：
  1. **运行信息**（只读定义列表）——见 §3 的响应字段。
  2. **用户名**——输入框 + 保存，调 `lib/settings.saveName()`；校验（非空、≤32 字、无控制字符）仍在服务端，前端只回显 `fields.name`。
  3. **主密码**——显示当前助记问题；内联「修改助记问题」表单；「修改主密码」按钮展开改密表单（§5）。未配置主密码时这张卡只给一个去 `/master-password` 的入口。
  4. **迁移状态**——仅当试解发现「仍有笔记用旧主密码保护」时出现，列出条数与「继续迁移」。

## 3. `GET /api/info` 与版本注入

新包 `internal/build/info.go`：

```go
var (
    Version   = "1.4.0" // 源码即当前版本，构建时可用 -X 覆盖；与应用文档版本号无关，各自演进
    GitHash   = ""
    GitDirty  = ""
    BuildDate = ""
)
```

`Describe()` 在 `GitHash/BuildDate` 为空时（`go run`、`just start`、`go test`）回落到 `debug.ReadBuildInfo()` 的 `vcs.revision / vcs.time / vcs.modified`，并把 `Version` 标成 `<version>（开发构建）`。信息页因此任何构建方式下都有内容，不会显示空白。

`internal/web/handlers_info.go` 响应字段：

```
version, git_hash, git_dirty, build_date, go_version,
goos, goarch, hostname, cpu_count,
mem_alloc_bytes, mem_sys_bytes,
started_at, uptime_seconds,
db_path, schema_version, addr, allow_remote
```

- `started_at` 在 `newApplication` 里记录，UTC 秒级文本 `2006-01-02T15:04:05Z`（遵守时间戳约定，可直接字符串排序）。
- `schema_version` 需要 store 新增 `SchemaVersion(ctx)`（读 `PRAGMA user_version`，目前只有 `migrate()` 内部知道）。
- 响应设 `Cache-Control: no-store`，与其余 API 一致。

`justfile:50` 的 `build` 改为注入四个变量。跨平台注意：just 在 Windows 走 powershell、Unix 走 bash，`git rev-parse --short HEAD` 与 `date -u +%Y-%m-%dT%H:%M:%SZ` 要按 `os()` 各写一份；不在 git 仓库内构建时命令失败必须退化成空串（`--short` 报错、`git` 不存在），不能让构建挂掉。

## 4. 后端接口契约

三个新端点，全部经过现有 `csrfProtect`（PUT 双提交）与 `no-store`。

| 端点 | 契约 |
| --- | --- |
| `GET /api/notes/encrypted` | `{notes: [{id, cipher_b64, kdf_params}]}`，一次返回全部加密笔记，不含标题与正文。避免逐条 `GET /api/notes/{id}` 的 N+1。Go 1.22 ServeMux 静态段优先于 `{id}` 通配，与 `GET /api/notes/{id}` 不冲突，用一条路由测试钉住。 |
| `PUT /api/notes/{id}/cipher` | 请求体只有 `cipher_b64` + `kdf_params`；store 新方法 `UpdateNoteCipher(ctx, id, cipherB64, params)` 只更新 `cipher / kdf_params / kdf_salt`，**不动 `title / content / category_id / rev / updated_at`**。非加密笔记返回 409 `not_encrypted`，不存在返回 404。 |
| `PUT /api/master-password/question` | 只改助记问题（trim 后非空、≤100 字符），返回更新后的 `MasterPassword`；未配置主密码时返回 409 `master_password_missing`。 |

**为什么是独立端点而不是 `PUT /api/notes/{id}` 加 `reencrypt: true` 标志**：`store.UpdateNote`（`internal/store/note.go:154`）无条件重写 `title`（`normalizeTitle` 要求非空）与 `updated_at`，加标志等于在同一个校验函数里塞第二套语义；独立端点的校验只剩「必须带密文与参数，其余不接受」。

**为什么不动 `updated_at`**：改一次密码不该让全部笔记在「按更新时间倒序」的列表里排到最前，那是对用户可见的假修订。

**改密不换全局盐**：`master_password.salt` 保持首次设置的值。换盐会让尚未迁移的笔记当场不可解（`notes.kdf_salt` 只是它的冗余副本，见 `kdfSaltFor` 注释，盐不变则该列无需迁移）。重加密时把每条笔记的 `kdf_params` 刷成当前应用参数，KDF 强度可以借改密提升。

## 5. 改密的浏览器端流程

新模块 `web/src/lib/reencrypt.ts`：纯函数 + 依赖注入（decrypt / encrypt / put 都可替换），可在 Vitest 里测；`InfoView` 只做状态编排。

1. 输入旧密码 → `deriveKey(旧, master_salt)` → **按清单顺序逐条试解，直到第一条能解开的为止**即视为验证通过。不能只试「任一条」就下结论：库里可能混着更早一次改密留下的笔记，先撞上那条就会把正确的旧密码误判成错误。全部试完仍无一可解才报「主密码不正确或数据已损坏」，且**不写入会话密钥**。库里没有加密笔记时跳过验证（没有可验证的东西），退化为「新密码两次一致」。
2. 输入新密码两次一致 → `deriveKey(新, master_salt)`。
3. `planMigration(inventory, newKey)`：先用**新**密钥逐条试解，能解开的判为 `already_done`，其余为待办。这一步让整条流程天然幂等。
4. 逐条 `decrypt(旧) → encrypt(新) → PUT /cipher`，带进度回调；单条失败记录后继续，不中断。
5. 提交完毕 `sessionKey.set(新密钥)`，再跑一次第 3 步复核：`remaining === 0` 提示成功；否则显示第 4 张卡。

**中途刷新/关页面**是本设计的核心场景，也是它比「显式状态列」更省事的地方：旧密钥随内存消失，但用户下次把**新密码**填进「旧密码」栏，第 3 步会把已迁移的笔记全判成 `already_done`，剩下的用新密码继续解——迁移自然续上，不需要任何持久化的迁移状态，也不需要 `key_version` 列与状态机。

## 6. 失败分类（穷尽，界面按类别说话）

| 类别 | 触发 | 界面 |
| --- | --- | --- |
| `done` | 本次成功重加密并提交 | 计入「成功 N」 |
| `already_done` | 新密钥已能解开，上次跑过了 | 计入「跳过 M」 |
| `skipped_gone` | `PUT /cipher` 返回 409 `not_encrypted` 或 404，迁移途中笔记被删 | 计入「跳过 M」 |
| `failed_old_key` | 旧密码解不开它——多半比这次输入的旧密码更早 | 列出，提示「这条笔记不是用刚才那个旧密码加密的」 |
| `failed_network` | 单条 PUT 抛错 | 列出，可重试 |

汇总文案「成功 N · 跳过 M · 失败 K」，`failed_old_key` 与 `failed_network` 都留在第 4 张卡里。

CSRF 令牌过期无需额外处理：`api.ts:63` 的 `sendJSON` 已对 403 重取令牌并重试一次，逐条 PUT 天然继承。

## 7. 安全不变量

- 服务端零密码学：新端点只搬运与校验字节，不解释密文；CLI 直读库解密仍是允许的扩展路径。
- 密码与派生密钥不落盘、不进任何浏览器存储：明文与 key 只活在函数局部变量，不进 toast、日志、localStorage/sessionStorage/IndexedDB。
- 日志不含正文/密码/Cipher：`accessLog` 从不记 body，`PUT /cipher` 的密文请求体因此不会入日志；新增日志只记 id 与 status。
- `no-store`：三个新端点统一设置。
- `{@html}` 仍只出现在 `web/src/components/Markdown.svelte`，信息页全部文本插值。
- **新增一条**：主密码可改，但仍不可找回。服务端永远不具备验证旧密码的能力，「旧密码是否正确」只由浏览器试解 GCM 认证标签判定；忘记旧密码时唯一出路仍是删除笔记。

## 8. 测试

**Go**

- `internal/build`：`Describe()` 的「已注入」与「回落到 vcs 信息」两分支。
- `handlers_info_test.go`：字段齐全、`no-store`、时间戳形如 `2006-01-02T15:04:05Z`。
- `store.UpdateNoteCipher`：`title/content/category_id/rev/updated_at` 全不变而 `cipher` 变了；非加密笔记被拒；不存在的 id 报 `ErrNoteNotFound`。
- `store.ListEncryptedCiphers`：只返回加密行、不含 `content`、明文笔记不出现在结果里。
- `store.UpdateMasterQuestion`：空串/超长被拒、未配置时 `master_password_missing`。
- 路由测试：`/api/notes/encrypted` 不被 `/api/notes/{id}` 通配吃掉。
- CSRF：三个新端点中两个 PUT 缺令牌时 403。

**前端 Vitest**

- `reencrypt.test.ts`（注入假 decrypt/encrypt/put）：§6 五种类别各自的路径；幂等续跑（第二次跑全是 `already_done`）；无加密笔记时跳过旧密码验证；`remaining` 汇总正确；**旧密码只能解开清单里靠后的那条时，验证仍须通过**（防止只试第一条的回归）。
- `router.test.ts`：`/info` 匹配，`/info?x=1` 也匹配（沿用 `matchRoute` 剥查询串的行为）。

**端到端**（Playwright 驱动 Edge headless，独立端口 + 独立库，沿用 `.run/` 约定）

建两条加密笔记 → 信息页读到版本与 git 信息 → 改用户名 → 改助记问题 → 错误旧密码被拦下且库里两条密文字节未变 → 正确旧密码跑完迁移 → 刷新后用新密码解锁两条 → **中途退出续跑**：只迁移一条就 reload，用新密码填旧密码栏续跑，最终 `remaining` 归零。

**合入门槛**：`just test` 与 `just check` 全绿；CHANGELOG 记入 `[Unreleased]`（Added / Changed / Security）。

## 9. 文档同步清单

| 文件 | 改动 |
| --- | --- |
| `docs/PRD.md` | 2.2 非目标移除「修改主密码」；5.3.1 第 4 条改写为「可改密、不可找回」；新增 FR 条目描述信息页与改密流程；Q5 结论更新；§14 候选项移除「修改密码」；版本 v1.3 → v1.4 |
| `docs/TECH_DESIGN.md` | 1.3 非目标移除「修改主密码」；1.2 安全不变量加第 7 条那句；§6 加密章节补改密与重加密流程；§1102「口令遗忘=无解」保留但补「改密需旧密码」；接口表加三个新端点；目录结构加 `internal/build/` |
| `AGENTS.md` | 硬性约定里补「主密码可改不可找回」与 `GET /api/info` 的 no-store |

## 10. 明确不做

- 不做密码找回、重置、后门、密钥托管。
- 不加 `key_version` 列与迁移状态机（试解已能得出同一结论）。
- 不做改密时的反向回滚（回滚自身也可能失败，风险只是换位）。
- 不引入读注册表/系统产品名的依赖。
- 不做并发保护：单用户单机，迁移途中笔记被删按 `skipped_gone` 处理。
