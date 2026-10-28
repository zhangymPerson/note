# 设计：用户信息页（运行信息 + 改名 + 改助记问题 + 改主密码）

日期：2026-10-27 · 状态：已实现（§5、§2.2 的续跑与半态语义按 `.run/info-e2e.mjs` 实测结果修订；PRD 升至 v1.4，TECH_DESIGN 升至 v1.4，AGENTS.md 已补硬性约定） · 覆盖 PRD v1.3 的 2.2 / 5.3.1 / Q5 / §14 中「修改主密码属非目标」的结论

## 1. 需求与对现有文档的偏离

| 项         | 本设计                                                          | PRD v1.3 / TECH_DESIGN 原案                                                                                       | 偏离原因                                                                   |
| ---------- | --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| 修改主密码 | 支持：旧密码验证 + 浏览器端批量重加密全部加密笔记               | 明确列为非目标（PRD v1.3 §2.2 非目标、§5.3.1 第 4 条、§13 Q5、§14；TECH_DESIGN v1.3 §1.3 非目标、§15.4 后续可选） | 用户明确要求                                                               |
| 密码找回   | 仍然不提供，任何形式都没有                                      | 不提供                                                                                                            | 不变。改密与找回是两件事：改密要求知道旧密码，服务端始终没有验证口令的能力 |
| 助记问题   | 可随时单独修改，不要求旧密码                                    | 只在首次设置时写入                                                                                                | 问题本就是明文存储、无解密价值                                             |
| 用户名     | 在信息页修改（复用已有 `PUT /api/settings/name`）               | 仅引导页设置                                                                                                      | 用户要求                                                                   |
| 首次配置   | 引导页两步：先名字，再**必填**的初始主密码                      | 引导页只收名字，主密码要另外找页设                                                                                | 用户要求；避免「想加密时才发现还没主密码」的断链                           |
| 运行信息   | 新增 `GET /api/info`：版本、git hash、构建日期、OS 与进程运行态 | 无此概念，二进制也没有版本注入                                                                                    | 用户要求                                                                   |

OS 信息粒度取「标准库能给的运行时信息」，不引入 `golang.org/x/sys` 之类的依赖去读注册表拿「Windows 10 Pro 19045」这种产品名。

## 2. 前端页面：信息页与引导页

### 2.1 信息页入口与卡片

- `web/src/components/Layout.svelte:42`：`.who` 由 `<span>` 改为 `<a class="who" href="/info">`，沿用同一份 `link(event, '/info')`，保留中键/右键新标签打开的既有行为。
- `web/src/router/router.ts`：`RouteName` 增加 `'info'`，`patterns` 增加 `{ name: 'info', pattern: /^\/info$/ }`；`App.svelte` 渲染新视图 `web/src/views/InfoView.svelte`。
- 页面四张卡，全部复用现有 `.card / .stack-form / .button / .hint` 类，不引入组件库或 CSS 框架：
  1. **运行信息**（只读定义列表）——见 §3 的响应字段。
  2. **用户名**——输入框 + 保存，调 `lib/settings.saveName()`；校验（非空、≤32 字、无控制字符）仍在服务端，前端只回显 `fields.name`。
  3. **主密码**——显示当前助记问题；内联「修改助记问题」表单；「修改主密码」按钮展开改密表单（§5）。未配置主密码时这张卡只给一个去 `/master-password` 的入口。
  4. **迁移状态**——仅当试解发现「仍有笔记用旧主密码保护」时出现，列出条数与「继续迁移」；按钮按失败原因决定预填新密码还是要求输入上一个主密码（§5 末）。

### 2.2 引导页改为两步：名字 → 初始主密码（必填）

**引导完成判据要改**。现在 `App.svelte:45` 只看 `!$settings.nameSet`，两步式之后这个判据漏一个半态：名字存好了、主密码还没设就刷新，引导页就不再出现，用户绕过必填进了主界面。改成「名字已设 **且** 主密码已配置」才算引导完成：

- `lib/settings.ts` 的 `SettingsState` 增加 `masterConfigured: boolean`，`loadSettings()` 并行拉 `GET /api/settings` 与 `GET /api/master-password`（后者已存在，返回 `configured / salt_b64 / question`，全是公开信息，不破坏「服务端零密码学」；`salt_b64` 正好是步骤 2 解锁形态派生密钥要用的）。任一失败仍按现有 `status: 'error'` 处理。
- `App.svelte` 的门槛改为 `$settings.nameSet && $settings.masterConfigured` 才渲染 `Layout`，否则渲染 `OnboardingView`；引导页内部按这两个布尔决定从哪一步开始（名字没设 → 步骤 1；名字已设但密码没配 → 直接步骤 2）。
- 页面标题与 `document.title` 的引导态判断同步用新判据，避免出现「还在引导却显示『XX的笔记』」。

页面版式沿用现有 `.onboarding`，标题下加「1/2 名字」「2/2 主密码」步骤指示。

**步骤 1 · 名字**——表单与校验完全不动（`saveName()` → `PUT /api/settings/name`）。成功后只推进本地状态到步骤 2，不刷新 settings（刷新会让引导页消失）。

**步骤 2 · 初始主密码**——字段与校验直接复用 `MasterPasswordView` 的表单逻辑：主密码 + 确认（`passwordMismatch()` 两次一致）+ 助记问题（≤100 字）+ 不可恢复勾选（未勾选则提交按钮禁用，同 FR-3.3 文案）。提交：`generateSaltB64()` → `POST /api/master-password` → `unlockKey(password, salt)` 把派生密钥放进会话内存 → 刷新 settings → 进主界面落在 `/`。**没有「暂时跳过」按钮**，两步走完才算引导完成。

**半态处理（步骤 2 挂载时先 `GET /api/master-password`）**，两种半态用同一分支覆盖：

| 半态           | 成因                                                                                                  | 步骤 2 表现                                                           |
| -------------- | ----------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| 未配置（正常） | 首次走完                                                                                              | 完整设置表单                                                          |
| 已配置         | POST 成功后、`unlockKey` 或刷新前被打断（含同一浏览器另开页签已完成引导、本页签的 settings 仍是旧值） | 变成「输入主密码解锁」单输入框：不再生成盐、不再 POST，只 `unlockKey` |

实测修正：原表列的第二种成因「老库先设过主密码但 `nameSet` 仍为 false」**不会**走到步骤 2——启动时 `masterConfigured` 已为 true，存完名字后 `saveName()` 就地翻转 `nameSet`，`decideStep` 直接返回 0，用户进主界面（无死路，也不需要解锁形态）。因此这条解锁分支唯一的真实成因是「POST 已成功但会话被打断/本地状态滞后」，端到端脚本用两个页签确定性地复现它。它不是死代码，别当成冗余分支删掉：缺了它，被打断的用户会重复 POST 撞 409 并永久卡在第二步。

这条幂等分支是必要的：`POST /api/master-password` 只允许一次，重复提交会 409 `master_password_exists`，没有它的话「密码设好了但流程被打断」就把用户永久卡在引导页第二步——只能删库重来。

**`/master-password` 页保留**：编辑器首次勾选加密时的 `?next=` 交棒路径仍需要它（引导判据改完后，正常路径不会再出现「已进主界面却没主密码」，但直接访问该 URL 与旧数据仍要能落地）。它的「已经配置过了」只读卡改为指向 `/info` 的主密码卡——改密与改助记问题都在信息页，那页不再重复一份表单。

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

| 端点                                | 契约                                                                                                                                                                                                                                                           |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/notes/encrypted`          | `{notes: [{id, cipher_b64, kdf_params}]}`，一次返回全部加密笔记，不含标题与正文。避免逐条 `GET /api/notes/{id}` 的 N+1。Go 1.22 ServeMux 静态段优先于 `{id}` 通配，与 `GET /api/notes/{id}` 不冲突，用一条路由测试钉住。                                       |
| `PUT /api/notes/{id}/cipher`        | 请求体只有 `cipher_b64` + `kdf_params`；store 新方法 `UpdateNoteCipher(ctx, id, cipherB64, params)` 只更新 `cipher / kdf_params / kdf_salt`，**不动 `title / content / category_id / rev / updated_at`**。非加密笔记返回 409 `not_encrypted`，不存在返回 404。 |
| `PUT /api/master-password/question` | 只改助记问题（trim 后非空、≤100 字符），返回更新后的 `MasterPassword`；未配置主密码时返回 409 `master_password_missing`。                                                                                                                                      |

**为什么是独立端点而不是 `PUT /api/notes/{id}` 加 `reencrypt: true` 标志**：`store.UpdateNote`（`internal/store/note.go:154`）无条件重写 `title`（`normalizeTitle` 要求非空）与 `updated_at`，加标志等于在同一个校验函数里塞第二套语义；独立端点的校验只剩「必须带密文与参数，其余不接受」。

**为什么不动 `updated_at`**：改一次密码不该让全部笔记在「按更新时间倒序」的列表里排到最前，那是对用户可见的假修订。

**改密不换全局盐**：`master_password.salt` 保持首次设置的值。换盐会让尚未迁移的笔记当场不可解（`notes.kdf_salt` 只是它的冗余副本，见 `kdfSaltFor` 注释，盐不变则该列无需迁移）。重加密把每条笔记的 `kdf_params` 统一刷成当前应用参数，这只是**归一化**、不是提升：旧密钥由调用方按当前 `KDF_PARAMS` 派生一次，从不读笔记各自的 `kdf_params`，所以**上调 `KDF_PARAMS.iter` 会让改造前的笔记在改密时全部判为 `failed_old_key`**（单条解锁仍按笔记自身参数派生，不受影响）。借改密真正提升 KDF 强度需要按笔记参数分别派生旧密钥，本版没做。

## 5. 改密的浏览器端流程

新模块 `web/src/lib/reencrypt.ts`：纯函数 + 依赖注入（decrypt / encrypt / put 都可替换），可在 Vitest 里测；`InfoView` 只做状态编排。

1. 输入旧密码 → `deriveKey(旧, master_salt)` → **按清单顺序逐条试解，直到第一条能解开的为止**即视为验证通过。不能只试「任一条」就下结论：库里可能混着更早一次改密留下的笔记，先撞上那条就会把正确的旧密码误判成错误。全部试完仍无一可解才报「主密码不正确或数据已损坏」，且**不写入会话密钥**。库里没有加密笔记时跳过验证（没有可验证的东西），退化为「新密码两次一致」。
2. 输入新密码两次一致 → `deriveKey(新, master_salt)`。
3. `planMigration(inventory, newKey)`：先用**新**密钥逐条试解，能解开的判为 `already_done`，其余为待办。这一步让整条流程天然幂等。
4. 逐条 `decrypt(旧) → encrypt(新) → PUT /cipher`，带进度回调；单条失败记录后继续，不中断。
5. 提交完毕 `sessionKey.set(新密钥)`，**先把迁移报告落地**（`report`/`remaining` 立刻可显示），再跑附带步骤：顺带改助记问题、重读清单用 `countRemaining` 复核。附带步骤各自 `try/catch`——复核请求失败时退回报告自带的计数，因为「迁移其实已经成功」这个结论不能被一次附带失败冲掉（否则用户拿真旧口令重试会得到「主密码不正确或加密数据已损坏」这句假话）。`remaining === 0` 提示成功；否则显示第 4 张卡，并按 `needsPreviousPassword(report)` 决定「继续迁移」预填什么（见 §5 末）。

**中途刷新/关页面**是本设计的核心场景，也是它比「显式状态列」更省事的地方：旧密钥随内存消失，第 3 步用新密钥试解会把已迁移的笔记全判成 `already_done`，因此不需要任何持久化的迁移状态，也不需要 `key_version` 列与状态机。

实测（`.run/info-e2e.mjs`）修正了本节原表述里的一处乐观：「把新密码填进旧密码栏，剩下的用新密码继续解」只对**响应丢失型中断**成立（迁移其实跑完了，只是页面没收到结果）。若中断发生在迁移**中途**，剩下的笔记仍由改动前的那一代口令保护，用新密码填旧密码栏解不开它们，会以 `failed_old_key` 列出（界面文案「这条笔记不是用刚才那个旧密码加密的」）；把它们推进完需要输入它们的**真实旧口令**。两种情况下未迁移条目都不会被写坏（实测断言：失败分支跑完后密文字节逐字不变），第 4 张卡如实区分这两类。所以「天然幂等」指的是**已迁移的条目不会被重做或损坏**，不是「任意中断都能靠新密码一键续完」。

界面据此把「继续迁移」该填哪个口令做成判定 `needsPreviousPassword(report)`：报告里有 `failed_old_key` 就不预填任何东西、按钮写成「继续迁移（请输入上一个主密码）」——预填刚设好的新密码等于把一个已被证明解不开的口令塞进输入框，让用户白花两轮 PBKDF2 再撞一次同样的失败；只有全是 `failed_network`（响应丢失型）时才预填新密码。

## 6. 失败分类（穷尽，界面按类别说话）

| 类别             | 触发                                                            | 界面                                             |
| ---------------- | --------------------------------------------------------------- | ------------------------------------------------ |
| `done`           | 本次成功重加密并提交                                            | 计入「成功 N」                                   |
| `already_done`   | 新密钥已能解开，上次跑过了                                      | 计入「跳过 M」                                   |
| `skipped_gone`   | `PUT /cipher` 返回 409 `not_encrypted` 或 404，迁移途中笔记被删 | 计入「跳过 M」                                   |
| `failed_old_key` | 旧密码解不开它——多半比这次输入的旧密码更早                      | 列出，提示「这条笔记不是用刚才那个旧密码加密的」 |
| `failed_network` | 单条 PUT 抛错                                                   | 列出，可重试                                     |

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
- 引导页两步：`nameSet=true` 但 `masterConfigured=false` 时仍渲染引导页且直接进 步骤 2；名字保存成功前不推进步骤；`GET /api/master-password` 返回已配置时步骤 2 渲染「解锁」形态且不触发 POST；未配置时渲染完整设置表单；勾选未打勾时提交禁用。

**端到端**（Playwright 驱动 Edge headless，独立端口 + 独立库，沿用 `.run/` 约定）

**空库引导**：全新库启动 → 步骤 1 只填名字就刷新，**必须仍在引导页且直接落在步骤 2**（验新判据）→ 设好密码进主界面；「POST 已成功、`unlockKey` 前刷新」这一半态实测要用**两个页签**才能确定性复现（页签 A 停在步骤 1，settings 是旧值；页签 B 先完成引导；A 再存名字 → 步骤 2 渲染「解锁」形态、`POST` 次数为 0），单页签里刷新无法得到「本地 nameSet=false 而库里已配置」的状态 → 输对密码进主界面。另测老库半态（`name_set: false` 但已配主密码）重启后填完名字直接进主界面，无死路。收尾断言 `master_password` 表只有一行、盐与首次设置的一致。

**改密主链路**：建两条加密笔记 → 信息页读到版本与 git 信息 → 改用户名 → 改助记问题 → 错误旧密码被拦下且库里两条密文字节与 `updated_at`/`rev` 未变 → 正确旧密码跑完迁移（密文全换、元数据不动）→ 刷新后用新密码解锁两条 → **中途退出**：用 `page.route` 让第二条 `PUT /cipher` 失败 → 状态卡「成功 1 · 跳过 0 · 失败 1（还有 1 条未迁移）」；刷新后用新密码续跑，已迁移判为 `already_done`、未迁移判为 `failed_old_key` 且**字节不变**（`remaining` 仍为 1，见 §5 的实测修正）；在浏览器里点状态卡的「继续迁移」，断言按钮文案是「继续迁移（请输入上一个主密码）」且点完旧密码栏为空（不预填刚设的新密码）；再用它的真实旧口令续跑，状态卡消失、两条都可用最新口令解锁。残留检索：本次的 4 个口令与 2 段加密正文都不出现在 `.db` 与 `-wal`，对照明文照常出现。

**合入门槛**：`just test` 与 `just check` 全绿；CHANGELOG 记入 `[Unreleased]`（Added / Changed / Security）。

## 9. 文档同步清单

| 文件                  | 改动                                                                                                                                                                                                                                                                                  |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/PRD.md`         | 2.2 非目标移除「修改主密码」；5.3.1 第 4 条改写为「可改密、不可找回」；新增 FR 条目描述信息页与改密流程；引导页章节补两步式（名字 → 必填初始主密码）与半态解锁的 FR/AC；3.x 页面清单「9 个主视图」改为 10 个（新增信息页）；Q5 结论更新；§14 候选项移除「修改密码」；版本 v1.3 → v1.4 |
| `docs/TECH_DESIGN.md` | 1.3 非目标移除「修改主密码」；1.2 安全不变量加第 9 条；§6 加密章节补改密与重加密流程（6.9）；§11.1 威胁模型「口令遗忘=无解」保留但补「改密需旧密码」；接口表加三个新端点与 `GET /api/info`；前端启动流程改写引导页判定（名字 + 主密码双条件）；目录结构加 `internal/build/`           |
| `AGENTS.md`           | 硬性约定里补「主密码可改不可找回」与 `GET /api/info` 的 no-store                                                                                                                                                                                                                      |

## 10. 明确不做

- 不做密码找回、重置、后门、密钥托管。
- 不在引导页提供「稍后再设主密码」：两步走完才算引导完成，半态由 §2.2 的解锁分支兜住。
- 不加 `key_version` 列与迁移状态机（试解已能得出同一结论）。
- 不做改密时的反向回滚（回滚自身也可能失败，风险只是换位）。
- 不引入读注册表/系统产品名的依赖。
- 不做并发保护：单用户单机，迁移途中笔记被删按 `skipped_gone` 处理。
