# Changelog

本项目所有值得关注的变更都记录在此文件中。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。只记录影响需求、架构、构建或使用方式的关键变更，不记录日常格式化与零散改动。

## [Unreleased]

### Added

- 全分支评审后的修复（改密收尾与续跑的可判定语义，实现过程与关键逻辑）：
  - 改密收尾不再掩盖成功：`InfoView.submitPassword` 原先在 `runMigration` 之后仍串「顺带改助记问题」与「重读清单复核」两步，任一步失败都落进同一个 `catch`，迁移状态卡因此根本不出现，界面只说「操作失败」——而密文其实已经全部换完。用户拿真正的旧口令重试时 `verifyOldPassword` 必然失败，界面就说出了「主密码不正确或加密数据已损坏」这句假话。现在 `sessionKey.set(新密钥)` 之后立刻 `report = result`、`remaining = result.remaining`，附带步骤各自 `try/catch`：改问题失败只追加「主密码已更新，但助记问题没改成功」，复核失败退回报告自带的计数。新增纯函数 `reencrypt.countRemaining(items, newKey, deps, fallback)` 承担这个退回（复核里非 `DecryptError` 的失败不再往上抛），用例先 RED（`countRemaining is not a function`）后 GREEN。
  - 「继续迁移」不再预填一个已被证明解不开的口令：`resume()` 原先无条件把新密码抄进旧密码栏。响应丢失型中断（迁移其实跑完了）这样是对的，但中途退出时剩下那些仍受**上一代**口令保护，预填新密码等于让用户白烧两轮 PBKDF2 再撞一次同样的失败。新增 `reencrypt.needsPreviousPassword(report)`（报告含 `failed_old_key` 即为真），InfoView 据此决定预填与文案：需要上一个口令时旧密码栏留空、按钮写成「继续迁移（请输入上一个主密码）」。两条用例同样先 RED 后 GREEN。
  - 文档收回一处做不到的承诺：规格与技术方案都写过「KDF 强度可以借改密提升」，但改密的旧密钥统一按当前 `KDF_PARAMS` 派生，从不读笔记各自的 `kdf_params`——上调 `iter` 会让改造前的笔记在改密时全部判为 `failed_old_key`（单条解锁不受影响，那条路径按笔记自身参数派生）。措辞改为「归一化，不是提升」并写明收紧需要什么（迁移按笔记参数分别派生旧密钥）；`EncryptedItem.kdf_params` 上加同源注释。
  - 补两处此前只写在评审里的边界：`PUT /cipher` 不带 `rev` 校验，迁移途中另一页签对同一条笔记的编辑会被写回覆盖（规格 §10 不做并发保护，接受并写明收紧办法）；顺带修正端到端断言计数（实际 58 项，此前文档写 57）。
  - 验证：`just test`（Go 4 个包 + 前端 12 个文件 100 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；`just build` 后重跑 `.run/info-e2e.mjs`——60 项断言全绿。「第二条 PUT 失败被记进迁移状态卡」「刷新后迁移状态卡不残留」「新密码填旧密码栏 → 已迁移那条 `already_done`、没迁那条 `failed_old_key` 且密文未损坏」「真实旧口令续跑 `remaining` 归零」这几条走的正是改写后的收尾分支；本次新增的两条断言直接在浏览器里点「继续迁移」，验按钮文案写明「请输入上一个主密码」且点完旧密码栏是空的（不预填刚设的新密码）。「改问题失败只追加提示、不掩盖改密成功」这条分支脚本没有制造该失败，由 `countRemaining` 用例与代码结构覆盖。

### Added

- 用户信息页端到端实测（`.run/info-e2e.mjs`，Playwright 驱动 Edge headless，60 项断言全绿）：
  - 环境：`just build`（必须走 just，否则 `ldflags` 不注入、看不到 commit 短 hash）产出 `note.exe`，脚本自己起停**三次**隔离实例——`--config .run/e2e-note.yaml --addr 127.0.0.1:8099 --db .run/e2e-info.db`。`--config` 单独隔离是必须的：改用户名会原子重写配置文件，不隔离就会把开发者真实 `config/note.yaml` 里的名字改掉。断言用 `node:sqlite` 只读打开同一个库，密文比对取 `cipher` BLOB 原始字节。
  - 引导：全新库走「填名字 → 刷新 → 仍在引导页第 2 步 → 设密码 → 进主界面」，`master_password` 只有一行、盐 16 字节且与 `GET /api/master-password` 的 `salt_b64` 一致，两次不一致被前端拦下时 `POST /api/master-password` 计数为 0，全程只 POST 一次；密码/密钥不进 `localStorage`/`sessionStorage`。
  - 已配置形态：脚本用两个标签页确定性地复现——页面 A 停在第 1 步（它的 settings 还是「主密码未配」），页面 B 先走完引导，回到 A 填名字后第 2 步就是解锁形态，`#master-password` 不再渲染、`POST` 计数为 0（重复 POST 会 409），库里仍是同一行同一盐。
  - 信息页：`/info` 直达；「Git 构建」行含构建时的短 hash，「服务版本」= `1.4.0 (<hash>, <UTC 构建日期>)`，运行平台/Go 版本/主机名/内存/数据文件/结构版本 4/监听地址/允许远程访问逐项核对；改用户名后界面、`GET /api/settings`、`name_set: true` 配置文件三处一致；改助记问题后 `salt_b64` 与首次设置相同。
  - 改密主链路：错误旧密码被拦下且两条 `cipher` 字节 + `updated_at` + `rev` 一字不变；正确旧密码跑完后两条密文都换、`updated_at`/`rev` 仍不变。中途退出用 `page.route` 只放行第一次 `PUT /api/notes/{id}/cipher`、AbortError 掉第二次 → 迁移卡如实显示「成功 1 · 跳过 0 · 失败 1（还有 1 条未迁移）」与「提交失败（Failed to fetch）」；reload 后迁移卡不残留（迁移状态本来就不落盘）。
  - **实测纠正了设计文档的一处乐观表述**：规格说中途关页面后「把新密码填进旧密码栏就能自然续上」，只对「PUT 已发出但响应丢了」的情形成立；真的只迁完一条时，剩下那条仍受**上一个密码**保护，填新密码会得到 `failed_old_key`（remaining 不归零），但已迁移那条判为 `already_done` 未被二次加密、未迁移那条密文字节与中止前完全相同（无损坏），卡片文案「这条笔记不是用刚才那个旧密码加密的」正是用户需要的提示。接着把真正的上一个密码填进旧密码栏续跑：只有没迁的那条被改写，remaining 归零，两条随后都能在详情页用新口令当场解开。
  - Security（明文残留）：改密全程用了 4 个口令、2 条加密正文与 1 条明文对照正文。扫描 `.run/e2e-info.db` 与 `-wal`：4 个主密码明文与 2 段加密正文都不在任何数据文件里，而明文对照组的正文确实出现在文件里——证明扫描方法本身有效、不是漏检。
  - 验证：`just test`（Go 4 个包 + 前端 12 个文件 96 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）在实测前后都通过。
- 用户信息页与浏览器端改密（实现过程与关键逻辑）：
  - 入口：顶栏 `.who` 由 `<span>` 改成 `<a href="/info">`（站内跳转走 `link()`，保留中键/新标签打开），新增路由 `/info`（`RouteName` 加 `'info'`、pattern `^\/info$`，带查询串仍按路径匹配）。
  - 运行信息卡：`web/src/lib/info.ts` 的 `buildRows(ServerInfo)` 把 `GET /api/info` 摊成 14 行中文键值（服务版本 / Git 构建 / 构建日期 / 运行平台 / Go 版本 / 主机名 / CPU 核心数 / 内存占用 / 本次运行时长 / 启动时间 / 数据文件 / 数据库结构版本 / 监听地址 / 允许远程访问）；缺失值统一显示「未知」而不是 `undefined`，`uptime_seconds` 摊成「X 小时 Y 分」，字节数按 B/KB/MB/GB 取一位小数，`allow_remote` 摊成「是/否」——都是纯函数，Vitest 直接断言。
  - 改密引擎：`web/src/lib/reencrypt.ts` 是纯函数 + 依赖注入（`decrypt/encrypt/putCipher` 全部可替换），`verifyOldPassword` 逐条试解、第一条成功即通过（不能只试第一条，库里可能混着更早一次改密留下的笔记），`planMigration` 用新密钥试解把已迁移的排除、使整条流程天然幂等，`runMigration` 把每条落定归入设计文档 §6 的五类（`done`/`already_done`/`skipped_gone`/`failed_old_key`/`failed_network`）并继续跑完其余；`remaining` = 后两类条数。明文只作局部变量，绝不进 `MigrationRow.message`（有用例断言整份报告序列化后不含正文）。
  - 改密流程（`InfoView` 只做编排）：拿盐 → 派生旧密钥 → 取密文清单 → 验证旧密码（清单为空直接通过）→ 派生新密钥 → 逐条 `decrypt(旧) → encrypt(新) → PUT /api/notes/{id}/cipher` → `sessionKey.set(新密钥)` → 重读清单再跑一次 `planMigration` 复核；旧密码验证不过时不动会话密钥、不写任何密文。第 4 张卡在 `remaining > 0` 时列出未迁移条目与原因并给「继续迁移」——中途关页面后把新密码填进旧密码栏续跑，已迁移的判为 `already_done` 原样跳过。
  - 助记问题与信息页改用户名：内联表单调 `PUT /api/master-password/question`，服务端拒绝（空/超 100 码点）时页面显示的仍是旧问题、只有输入框标红；改密表单可留空顺带更新问题。
  - 引导页改两步式：判据抽成纯函数 `decideStep(nameSet, masterConfigured): 0|1|2`（`web/src/lib/onboarding.ts`），`App.svelte` 的门槛与 `document.title` 共用同一判定。步骤 1 存名字成功后只本地推进到步骤 2、不重读设置；步骤 2 挂载时读 `GET /api/master-password`，未配置 → 完整设置表单（密码 + 确认 + 助记问题 + 不可恢复勾选，主密码必填、无「跳过」），已配置（上次 POST 成功但解锁前刷新）→ 只渲染解锁输入框、只 `unlockKey` 绝不重复 POST。`settings.loadSettings()` 改为 `Promise.all` 并行读 `/api/settings` 与 `/api/master-password`，任一个失败整体进 `error` 态——引导判据不能建立在半套数据上。
  - `MasterPasswordView` 的「已经配置过了」只读卡改为指向 `/info`，文案随之从「无法再次修改」改成「可到信息页修改」。
  - 验证：`just test`（Go 66 项 + 前端 96 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；新增 `reencrypt.test.ts` 15 项、`info.test.ts` 9 项、`settings.test.ts` 4 项（含「主密码接口失败必须整体进错误态」这条先用旧实现跑出 RED）、`notes.test.ts` 4 项、`router.test.ts` 1 项、`onboarding.test.ts` 3 项。
- 信息页后端：构建信息端点、加密笔记密文清单/替换、助记问题单独修改（实现过程与关键逻辑）：
  - 构建信息：新增 `internal/build`，`Version`/`GitHash`/`GitDirty`/`BuildDate` 四个包级变量由 `justfile` 的 `ldflags` 变量用 `-X` 注入（按 `os()` 分支，Windows 走 PowerShell、其余走 bash 命令替换）；未注入时（`go run`、`go test`）`Describe()` 回落到 `runtime/debug.ReadBuildInfo` 的 `vcs.revision`/`vcs.time`/`vcs.modified`，`Label()` 此时输出「1.4.0（开发构建）」而不是空括号。just 在 Windows 上把命令包成 `cmd /C "powershell -Command …"` 且只保留最后一行输出，所以 PowerShell 分支必须以裸 `$a` 结尾。
  - 接口层：`GET /api/info` 返回 version / git_hash / git_dirty(bool) / build_date / build_label / go_version / goos / goarch / hostname / cpu_count / mem_alloc_bytes / mem_sys_bytes / started_at / uptime_seconds / db_path / schema_version / addr / allow_remote —— 全是标准库运行时与配置值，不含正文、密码、密钥或令牌；`application` 新增 `startedAt`（进程启动 UTC 时刻），`Store.SchemaVersion` 读 `PRAGMA user_version` 供页面展示已应用迁移编号。
  - 密文迁移端点：`GET /api/notes/encrypted` 只输出 `{id, cipher_b64, kdf_params}`（空清单序列化成 `[]` 而非 `null`，前端不必判空），`PUT /api/notes/{id}/cipher` 只替换 `cipher` 与 `kdf_params`，元数据（title/content/created_at/updated_at/rev/category_id）逐字不变——这是浏览器端批量重加密「改一次口令却不动用户看到的修改时间」的接口基础。路由把静态段 `encrypted` 注册在通配 `GET /api/notes/{id}` 之前，`go test` 断言清单请求没被通配吃掉。
  - 存储层：`ListEncryptedCiphers`（按 `created_at, id` 稳定排序，改密过程中新增/删除笔记不会让分页错位）与 `UpdateNoteCipher`；后者先在事务外做只读探针，把「笔记不存在」和「笔记未加密」这两类错误明确区分成 `ErrNoteNotFound` / `ErrNotEncrypted`（`db.SetMaxOpenConns(1)` 下事务内再开第二条查询会死锁，全局盐也必须事务外取），错误码映射为 404 `not_found` / 409 `not_encrypted` / 400 `validation_failed`。`decodeCipher` 从 `normalizeBody` 抽出复用，base64 与 29–65536 字节的密文长度校验两处语义一致。
  - 助记问题：`Store.UpdateMasterQuestion` 只 UPDATE `question` 一列，盐与 `created_at` 不动（改问题不能顺带换盐，否则已有密文全部解不开），空问题与超 100 码点分别返回字段错误；`PUT /api/master-password/question` 走 CSRF 保护。`master_password_exists` 的旧文案「无法再次修改」改成「主密码已设置，如需更换请到信息页的主密码卡片」——引导页设的主密码如今确实可以在信息页换掉。
  - 验证：`just test`（Go 66 项 + 前端 60 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；`just build` 产出的真实二进制里 `GET /api/info` 返回 commit hash 与 UTC 构建日期，`go run` 返回「开发构建」；`note_cipher_api_test.go` 覆盖清单不含 title/content、PUT 缺 `kdf_params` 与非法 base64 各返回对应字段错误、对明文笔记返回 409、不存在的 id 返回 404、不带 `X-CSRF-Token` 的 PUT 返回 403 且库里密文未变。
- 笔记 CRUD + 分类管理 + 全局主密码浏览器端加密 + Markdown 编写预览（实现过程与关键逻辑）：
  - 数据层：迁移 `sql/0004_note_app.sql` 新增 `categories`（保留 `id=1`「未分类」、`name` NOCASE 唯一）、`master_password`（单行，只存 16 字节公盐 `salt` 与助记问题 `question`，密码与派生密钥都不落库）以及 `notes.category_id` 外键、`notes.cipher` BLOB、`notes.kdf_params` JSON。「不选分类」在库里存成 `category_id IS NULL`，与 `id=1` 行展示等价。
  - 存储层：`internal/store/category.go` 分类 CRUD——删除时若仍有笔记返回 `category_not_empty` 并带 `fields.note_count`，`id=1` 禁止改名与删除（`category_reserved`）；`internal/store/note.go` 笔记 CRUD 与分页搜索（`q` LIKE 通配符转义、按分类过滤、`updated_at` 倒序）；`internal/store/master.go` 主密码一次性设置（重复设置 `master_password_exists`，未设置时加密返回 `master_password_missing`）。
  - 接口层：`GET/POST/PUT/DELETE /api/notes[/{id}]`、`GET/POST /api/categories`、`PUT/DELETE /api/categories/{id}`、`GET/POST /api/master-password`。正文与密文必须二选一（`content` ≤ 2000 码点；`cipher_b64` StdEncoding、29–65536 字节；`kdf_params` 校验 algo/iter/keylen）；已加密笔记不允许改回明文（409 `encrypted_immutable`）。服务端全程零密码学代码，只校验与透传，因此 CLI 直读 SQLite 解密是可行的扩展路径。
  - 浏览器端加密：`web/src/lib/crypto.ts` 用 WebCrypto `PBKDF2-HMAC-SHA256`（NFC 口令 + 库中公盐，iter 150000、keylen 32）派生 `AES-256-GCM` 密钥（`extractable:false`），密文格式 `nonce(12) ‖ ciphertext ‖ tag(16)`，AAD 绑 `note:v1:<noteId>` 防跨笔记挪用密文；派生密钥只存在 `web/src/lib/session-key.ts` 的页面内存，无服务端会话、无自动过期，锁定即 `set(null)` 并清空草稿。派生失败、tag 校验失败、编码错误统一收敛为「密码错误或数据损坏」，不区分原因以免泄露可判定信息。
  - Markdown：`web/src/lib/markdown.ts` + `web/src/components/Markdown.svelte` 用 `marked` 解析、`DOMPurify.sanitize` 清洗后才 `{@html}`，外链补 `rel="noopener noreferrer"`；后端只保存原始 Markdown 文本，编辑器左侧写原文、右侧实时预览，`web/src/lib/draft.ts` 单槽内存草稿保证解锁表单回填不会吞掉已输入正文。
  - 界面：新增/编辑 `NoteEditorView`、列表 `NoteListView`（分类筛选 + 分页 + 删除确认）、详情 `NoteDetailView`（已加密笔记先解锁）、设置主密码 `MasterPasswordView`、忘记密码 `ForgotPasswordView`、分类管理 `CategoryManagerView`；路由 `/notes`、`/notes/new`、`/notes/:id`、`/notes/:id/edit`、`/notes/:id/forgot`、`/master-password`、`/categories`。
  - 验证：`just test`（Go 5 个包 + 前端 54 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；Playwright 驱动 Edge 在隔离实例（`127.0.0.1:8099` + `.run/e2e-notes.db`）跑通 29 项端到端断言；`node:sqlite` + `node:crypto` 写的独立 CLI 直读同一个库解密，正文与浏览器原文逐字一致、错误密码被 GCM 拒绝；二进制扫描确认加密正文未落盘（同库明文笔记的对照串确实出现在 WAL 中，说明扫描方法有效）。修补后又跑了一遍浏览器实测（`.run/fix-verify.mjs`，26 项断言全绿）：两次密码不一致/漏填都被拦下且没写出半成品笔记、交棒设置主密码后草稿原样回到编辑页、另一篇笔记的编辑器拿不到这份草稿、库里 `is_encrypted=1` 且 `kdf_params` 就是应用参数、手改该行 `iter` 后连正确密码也报「密码错误或数据损坏」（改回即恢复）、错误密码与损坏仍是同一句提示、未分类 chip 计数与筛选都含两篇、分类新增与改名撞名都显示「该分类名已存在」、`q` 恰好 200 字通过 / 201 字 400 + `fields.q`。
- 前端三主题切换（亮色 / 暗色 / 护眼，实现逻辑与关键决策）：
  - 机制：`web/src/lib/theme.ts` 提供纯函数 `resolveTheme(存储值, 系统是否暗色)`——合法存储值优先，否则跟随 `prefers-color-scheme`；writable store 订阅时把结果写入 `<html data-theme>`，`setTheme` 同步存 `localStorage('note.theme')`。纯前端偏好，不落 `note.yaml`、不加后端 API。
  - 样式：`app.css` 移除 `prefers-color-scheme` 暗色块，改为 `:root[data-theme=...]` 三块变量——亮/暗直接采用 GitHub Primer 官方色板（亮 `#ffffff/#f6f8fa/#d0d7de/#1f2328/#0969da`，暗 `#0d1117/#161b22/#30363d/#e6edf3/#1f6feb`），热力图绿阶同步 GitHub 贡献图五级色；护眼为自定低饱和米绿纸色（`#e9efdf` 底、深橄榄字、苔绿 accent）。
  - 入口：`ThemeSwitch.svelte` 分段控件（亮色/暗色/护眼，`aria-pressed` 标记当前项、键盘可达）挂在 `Layout` 顶栏右上角；引导页不放置，主题在模块加载时已生效。
  - 验证：新增 `theme.test.ts` 3 项（前端共 14 项）与 `just check` 通过；Playwright 驱动 Edge 实测——初始跟随系统、三主题点击切换后 `data-theme`/前景色/`aria-pressed` 全部正确、刷新后保持护眼、键盘 Enter 可切换、全新上下文系统暗色回退 dark；截图见 `.run/shot-{light,dark,eye-care}.png`。
- `just pull` / `just push` 多远程同步任务：
  - `pull` 先 `git fetch --all --prune` 再 `git pull --rebase`；`push` 复用同样的 fetch + rebase 前置，rebase 有冲突时该行非零退出、just 立即中止，不会带着冲突推送。
  - 推送遍历 `git remote` 的全部远程（本仓库为 gitcode/gitee/origin 三个），Windows 用 PowerShell `ForEach-Object`、Unix 用 `xargs -n1`，按 `os()` 分发。
  - 验证：`just --dry-run pull|push` 展开命令正确；`just test`（Go 22 项 + 前端 11 项）与 `just check` 通过，未实际执行网络推送。
- `just start` / `just stop` / `just restart` 后台开发任务（跨平台脚本，实现逻辑与关键决策）：
  - `restart` 用 just 依赖顺序 `restart: stop start` 实现，先杀旧进程树再拉起；实测重启后前后端均 200。
  - 接口层：`justfile` 按 `os()` 分发——Windows 调 `scripts/dev.ps1`，Unix 调 `scripts/dev.sh`；start 后台拉起 `go run ./cmd/note`（:8080）与 `npm --prefix web run dev`（:5173，/api 已代理到后端），stop 按 `.run/*.pid` 停止。
  - 守护逻辑：首次 start 自动补 `npm ci` 与前端 build；PID 文件存在且进程存活则跳过重复启动；stop 用 `taskkill /T /F`（Windows）或递归 `pkill -P`+`kill`（Unix）杀整棵进程树，因 `go run` 与 npm 都会派生子进程。
  - 关键决策：Windows 端进程经 WMI `Win32_Process.Create` 创建，父进程是 WmiPrvSE，不继承 just 传入的 stdout 管道句柄——此前 `Start-Process` 方案让存活子进程持有该句柄，调用方永远等不到管道 EOF 导致 `just start` 挂起；日志重定向改由子 cmd 内部完成，统一写 `.run/<name>.log`。
  - 约定：`.run/` 已加入 `.gitignore`；`dev.ps1` 含中文输出，必须保持 UTF-8 BOM 以兼容 PowerShell 5.1。
  - 验证：`just start` 约 9 秒返回，后端与前端均探测 200；`just stop` 约 6 秒返回且 5173/8080 端口释放；Go 22 项、前端 11 项测试与 `svelte-check`、`go vet` 全部通过。
- 笔记表扩展字段（迁移 `sql/0003_note_fields.sql`，实现逻辑与关键决策）：
  - 新增 `kind`（笔记类型，`TEXT NOT NULL DEFAULT 'note'`，取值由应用层维护、新增类型无需迁移）、`remark`（纯文本备注，默认空串）、`extra`（JSON 扩展备注，列级 `json_valid` CHECK，NULL 表示无）三个用户要求的字段。
  - 补充 `rev`（修订号，应用侧编辑时 +1，用于乐观并发与变更检测）与 `last_visited_at`（最近查看时间，UTC 秒级文本，配合 activities 支撑"最近打开"排序）。
  - 置顶、回收站、标签等候选字段因与 PRD 2.2 非目标冲突未引入；提醒时间、颜色、来源等未定型信息统一先进 `extra` JSON，稳定后再提升为独立列。
  - 迁移遵循 SQLite `ALTER TABLE ADD COLUMN` 逐列追加约束；`store_test` 断言版本 3 并验证默认值回填、非法 JSON / 空 kind / 非正 rev 被 CHECK 拒绝。
  - 验证：`go vet` + Go 22 项 + 前端 11 项测试全部通过。
- 用户命名与使用统计首页（实现过程与关键逻辑）：
  - 配置层：`config/note.yaml` 新增 `user.name` 与 `user.name_set` 两个配置；`internal/config` 解析该段并新增 `SaveUserName`，以同目录临时文件 + rename 原子写回，保证重启后读回姓名与门禁状态不丢。
  - 数据层：新增迁移 `sql/0002_activity.sql` 建 `activities` 表（`kind='search'`、`query`、UTC 秒级 `created_at`，含按天与按词两个索引）；`internal/store/activity.go` 提供 `SearchNotes`（LIKE 通配符转义防注入扩大匹配，查询即记录活动）、`RecordSearch` 与 `GetStats`（总次数、活跃天数、Top 查询词、近 N 天按天计数）。
  - 接口层：`internal/web` 重构为 `application` 结构承载配置与存储，注册 `GET/PUT /api/settings(/name)`、`GET /api/notes?q=`、`GET /api/stats`；新增 `csrfProtect` 中间件对所有状态变更类 API 做 Cookie 与 `X-CSRF-Token` 恒定时间比对，缺失或不符返回 403。
  - 前端：`lib/api.ts` 统一 CSRF 令牌获取与 403 自动刷新重试；`lib/settings.ts` 启动时拉取设置，`App.svelte` 在 `name_set=false` 时整屏渲染 `OnboardingView` 引导输入姓名并写回配置；路由 `/` 改为统计首页 `HomeView`（问候、搜索框、使用账簿），笔记列表移至 `/notes`。
  - 可视化：`lib/stats.ts` 的 `buildHeatmap` 生成 53 周 × 7 行（周一开头）GitHub 提交图风格网格，`levelFor` 按 0/1-2/3-5/6-9/10+ 分五级；`ActivityHeatmap.svelte` 纯 CSS Grid 渲染，月份/星期标签、少→多图例、`title` 悬停提示，色阶为墨绿渐变并支持暗色模式。
  - 设计决策：按 frontend-design 技能采用「纸上墨迹」方向——冷纸底色 `#f3f4f1`、深青墨主色、衬线问候语（系统中文字体栈，零外部字体），热力图为全页唯一大视觉，统计用点线账簿行而非卡片堆砌。
  - 验证：Go 21 项、前端 11 项测试与 `svelte-check` 全部通过；单二进制实测完整链路（设名 → 重启持久化 → 搜索 3 次 → 统计与 UTF-8 落盘字节正确）。
- 产品需求文档与技术方案，明确单向加密笔记的安全边界与数据流。
- 可运行项目骨架：Go 入口、分层配置、SQLite 自动建库与迁移、内嵌前端、CSRF 与安全中间件、优雅退出。
- `config/note.yaml` 默认配置，支持内置默认值、YAML、命令行参数三级覆盖，YAML 严格解析。
- `sql/0001_init.sql` 初始表结构与 `sql/embed.go` 迁移嵌入，启动时按 `user_version` 增量执行。
- Svelte 5 + Vite + TypeScript 前端骨架，包含 History 路由、页面占位、类型检查与 Vitest 测试。
- `docs/BUILD.md` 构建指南，记录从命令行生成脚手架到单二进制验证的完整流程。
- 跨平台 `justfile`：`install`、`frontend`、`build`、`test`、`check`、`format` 任务，Unix 使用 bash、Windows 使用 PowerShell。

### Changed

- 新增根目录 `.gitattributes` 统一换行符（`* text=auto eol=lf`；`*.ps1 text eol=crlf` 保留 Windows PowerShell 惯例）：仓库与检出工作区一律 LF，`eol` 属性优先级高于系统级 `core.autocrlf=true`，不改任何 git 配置、也不影响其他仓库。一次性把 19 个工作区 CRLF 文件中的 18 个转为 LF（`scripts/dev.ps1` 除外），并顺带清掉 40 个「字节与索引一致、仅 stat 脏」的幽灵 M 标记（对字节一致的文件 `git add` 只刷新 stat 条目，暂存区零内容，索引 blob 不变）。验证：`git ls-files --eol` 复核全库 107 个 `i/lf w/lf` 加 `dev.ps1` `i/lf w/crlf`；`git check-attr` 确认属性生效；`just test`（内含 `go fmt` 与 prettier 全仓库格式化）跑完后 status 仍只有既有的 42 个未提交工作文件——格式化不再产生换行符虚假 diff。`just test` 与 `just check` 通过。
- 文档同步到 v1.4（`docs/PRD.md`、`docs/TECH_DESIGN.md`、`AGENTS.md`、设计文档与实现计划），把已验证的信息页与改密实现写回需求与架构文档：
  - PRD：版本升到 v1.4，新增 §2.5「v1.4 实现偏离」五行对照表（改密 / 助记问题 / 用户名 / 首次配置 / 运行信息）与收敛指针段落；§2.2 非目标明确「改密要求旧密码，与找回是两件事」；§2.3 补「修改密码不算恢复」；§5.3.1 第 4 条改写为可改密不可找回；§5.3.2 由「POST 仅首次配置」推出 FR-3.16（逐条试解、第一条成功即通过、空清单跳过验证）、FR-3.17（只替换 `cipher`/`kdf_params`，元数据与全局盐逐字不变）、FR-3.18（可中断可续跑并如实列出混合状态）、FR-3.19（改问题不换盐）；新增 §5.3.6 用户视角七步改密流程（原文案节顺延为 §5.3.7）；§5.4 路由与 API 表补 `/info` 与四个新端点；新增 §5.6 首次引导 FR-5.1~5.6 与 §5.7 用户信息页 FR-6.1~6.6；§6 页面数 9 → 10；§3.2 加 US-10（换密码后仍可读）与 US-11（复制二进制到另一台机器）；§4 术语表改主密码/助记问题两行、加「重加密迁移」；§9 改写 AC-08/AC-33、新增 AC-35 ~ AC-44；§10 加 M5 里程碑；§11.1 目录结构加 `internal/build/` 与各新文件；§11.2 加两条手工检查；附录 B 三条改写；§13 Q5 结论翻转、§14 移除「修改密码」候选项。
  - TECH_DESIGN：版本升到 v1.4；§1.2 新增第 9 条不变量（旧口令是否正确只由浏览器试解 GCM 标签判定，服务端始终没有验证口令的能力，密文请求体不入日志）；§1.3 非目标与 §15.4 移除「修改主密码」；§2.3 目录树补 `internal/build/info.go`、`handlers_note_cipher.go`、`handlers_info.go`、`reencrypt.ts`/`info.ts`/`onboarding.ts`；§4.3 路由清单此前停留在 v1.3 已删除的 `/encrypt`、`/unlock`、`/lock`、`POST /api/lock` 上，与 §8.3「不存在解密端点」自相矛盾，按 `internal/web/server.go` 实际注册的 20 条重写；§4.4 处理器表、§8.3 表、§8.5 错误码表（加 `not_encrypted`、`master_password_missing`）同步；§5.4 补 `ListEncryptedCiphers`/`UpdateNoteCipher` 的事务外只读探针（`db.SetMaxOpenConns(1)` 下事务内第二条查询会死锁）；§5.5 补「改密只在 WAL 留旧密文，不产生明文残留」；新增 §6.9 六步迁移流程与设计取舍（为何独立端点、为何不动 `updated_at`/`rev`、为何不换全局盐、为何不加 `key_version`，含实测边界）；新增 §8.6 运行信息响应（18 字段样例 + `-X` 注入与 `vcs` 回落）；§9.2/§9.3/§9.5/§9.6/§9.7 前端章节按 `decideStep` 判定与 `Promise.all` 半套数据即错误态重写；§10.3/§10.4 构建章节同步 `justfile` 新 ldflags（含 Windows 只保留最后一行输出的坑）；§11.1 威胁模型加「迁移途中关页面」「信息页暴露运行信息」两行；§12 测试表补四份新 Go 测试与三份前端用例、并描述 `.run/info-e2e.mjs` 三次启动 60 项断言；§15.5 新增 M5；§16 改写 ADR-02/ADR-11，新增 ADR-16（试解即校验器，不加 `key_version`）、ADR-17（独立端点 + 不动元数据 + 不换盐）、ADR-18（ldflags + `vcs` 回落）、ADR-19（两步引导 + 半态解锁分支）。
  - `AGENTS.md`：目录结构加 `internal/build/`；硬性约定加三条——主密码可改不可找回且 `PUT /api/notes/{id}/cipher` 有意不动 `title/content/category_id/rev/updated_at`（不要「顺手」对齐成 `PUT /api/notes/{id}`）、`GET /api/info` 必须 `no-store` 且 git 信息不可用时退化为空值而不是让构建失败、引导判据 `decideStep(nameSet, masterConfigured)` 必须与 `document.title` 共用且第二步已配置时只解锁不重复 POST。
  - 设计文档与实现计划：状态行改为「已实现」；§2.2 半态表补上实测复现路径（同一浏览器另开页签已完成引导、本页签 settings 仍是旧值）并注明该分支不是死代码；§5 续跑语义按实测改写（响应丢失型中断可用新密码续跑；真的只迁完一条时剩下那条仍受上一代口令保护，填新密码得到 `failed_old_key`，需真实旧口令续跑）；§8 端到端小节改为实际脚本行为；文档内引用从行号改为章节号（`§1102` → `§11.1`，AC-33 的交叉引用从 AC-38 改为 AC-39/AC-40）。
  - 验证：引用逐条与代码核对（`masterSaltBytes`/`maxNoteBody`/`kdfParamsError`/`minCipherBytes`/`maxCipherBytes`、`ListEncryptedCiphers`/`UpdateNoteCipher`/`UpdateMasterQuestion`/`SchemaVersion`/`decideStep`/`planMigration`/`runMigration`/`verifyOldPassword`、`internal/build/info.go` 与三个新前端模块均存在且命名一致；`handleUpdateMasterQuestion` 确实落在 `handlers_categories.go`，与目录树注释「分类与主密码端点」相符）。`just test` 与 `just check` 通过。
- `docs/PRD.md` 与 `docs/TECH_DESIGN.md` 升到 v1.3，按已验证实现逐项重写偏离部分：Argon2id → WebCrypto PBKDF2-HMAC-SHA256（纯浏览器无 WASM 成本）、服务端加解密与会话 Cookie（`note_sess`、TTL、401 家族）→ 派生密钥只留页面内存且无服务端会话、`{@html}` 全面禁止 → `Markdown.svelte` 单点例外且输入必过 `DOMPurify`、错误码矩阵修正（分类名重复是 400 而非 409、字段是 `fields.note_count` 而非 `fields.count`、无 415）、正文上限 1 MiB → 2000 码点；性能表区分实测数值（前端 JS 163.61 kB / gzip 58.34 kB）与未测项，新增 ADR-01…ADR-15 与 Q1–Q7 结论，明确「未执行」清单（Firefox/Chrome 实测、10k 笔记性能、`go test -race`、fuzz）。
- `AGENTS.md` 安全不变量改写：`{@html}` 例外表述、密码与派生密钥不进任何浏览器存储、服务端零密码学代码与 CLI 直读解密定位。
- 技术方案补充配置目录、数据库自动检测与迁移流程、`sql/` 迁移目录约定。
- 前端构建输出重定向到 `internal/web/dist`，由 `go:embed` 嵌入，部署不再需要 Node/npm。
- Go 模块版本提升到 1.26，SQLite 使用纯 Go 驱动，构建全程关闭 CGO。

### Fixed

- 首页「近一年使用频率」热力图的月份标签显示不全：`.heat-field` 的周列只有 `calc(var(--cell) + 3px)`（14px）宽，`.heat-month` 作为网格项默认 `justify-self: stretch` 被压成同样宽度，而 CSS 自动换行允许在数字与汉字之间断行，于是「10月」折成两行；行高只有 18px 且 `align-self: end`，上面那行数字被 `.heatmap` 的 `overflow-x: auto`（隐含 `overflow-y: auto`）裁掉，页面上只剩一个「月」字。给 `.heat-month` 加 `white-space: nowrap; width: max-content; justify-self: start`，标签按内容宽度单行渲染并向右溢出到第 1 行的空白区（相邻月份至少隔 4 列约 56px，不会重叠）。
  - 验证：`just test`（Go 50 项 + 前端 60 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；用无头 Edge 对同一页面截图对比——修复前月份只剩「月」且数字行被裁，修复后 `10月 12月 1月 … 10月` 完整单行显示，且截图取自 `just build` 产出的内嵌前端二进制。
- 勾选「用主密码加密」的新笔记在保存后跳到「页面不存在」：编辑器交棒用的是站内跳转 `navigate('/master-password?next=…')`，而 `router.matchRoute()` 拿整串（含查询串）去匹配 `^/master-password$`，匹配失败落到 `notFound`。第一次加密这条主路径因此在真实浏览器里完全走不通（直接地址栏输入或用新标签打开反而正常，所以单元测试与此前的 e2e 都没发现）。`matchRoute()` 改为先剥掉 `?`/`#` 之后的部分再匹配路径；`router.test.ts` 先以 `/master-password?next=…`、`/notes/abc?from=list`、`/notes/abc#top` 三例复现（RED：`notFound`）再修复（GREEN）。

- 首次加密可能把笔记永久锁死：主密码已配置但本会话还没有密钥时，编辑器只要求「输入非空」就用它派生密钥并加密——打错一个字母照样保存成功，刷新后再也解不开，而且当场没有任何异常。现在这条路径必须两次输入主密码且比对一致（`session-key.passwordMismatch`）才继续；已有密文可当场试解的路径不变（由 GCM 标签验证）。
- 浏览器忽略笔记自己声明的 KDF 参数：`kdf_params` 列被写入却从未被读取，手改库里的 `iter` 不影响解锁，PRD AC-23 与技术方案 §6.2/§6.3 的承诺都落空。新增 `crypto.paramsFromNote()`：按该笔记声明的参数派生，越界（`iter` 不在 [100000,1000000]、`keylen≠32`、`algo≠pbkdf2`、JSON 畸形）一律按数据损坏拒绝解密；详情页与编辑器的解锁都改用它，重加密时把同一份参数写回去。顺带修正「`kdf_salt` 供客户端比对」的说法——接口从不返回该列，它只是满足 0001 `CHECK` 的冗余副本。
- 内存草稿会被别的笔记消费：`editorDraft.take()` 在挂载时无条件取走草稿，随后 `loadNote()` 用库里的正文覆盖，结果是「跳去设置主密码时存下的草稿」在下次打开任意已有笔记时被吃掉并注入那条形编辑器的第一条笔记（保存即覆盖）。草稿现在带 `noteId`，`take(noteId)` 只交给同一条笔记的编辑器；离开编辑器（组件卸载）清空草稿，只有跳 `/master-password?next=…` 的交棒除外。
- 分类重名的状态码从 400 改为 409 `category_exists`（仍带 `fields.name`）：名字被占用是状态冲突，与 `category_not_empty`、`id_conflict`、`category_reserved` 同族；此前只有字段级 400，客户端只能靠字符串猜是「太长」还是「重名」。同时补齐此前缺失的用例：改名撞进只差大小写的同名（store 与 API 两层）、`%`/`_`/`\` 作为搜索词必须按字面匹配、前端正文长度改由 `notes.countCodePoints()` 计算并断言与后端 2000 码点上限同源。

- 「未分类」既看不见也统计不到：库里未选分类的笔记存的是 `notes.category_id IS NULL`，而 `ListCategories` 只按 `n.category_id = c.id` 计数、`ListNotes` 只按 `n.category_id = ?` 过滤，导致分类 chip 永远是 0、「全部」少算、点「未分类」列表为空。改为默认分类（`id=1`）计数与筛选都并入 `IS NULL`（`LEFT JOIN ... OR (c.id = ? AND n.category_id IS NULL)`、`(n.category_id = ? OR n.category_id IS NULL)`）。先以 `TestDefaultCategoryCoversUncategorizedNotes` 复现（RED：`NoteCount:0`，期望 2）再修复（GREEN）。
- 编辑已加密笔记时正文一片空白：`NoteEditorView` 拉到笔记后只置 `cipher`、没有尝试用会话里的派生密钥解密，已解锁的会话也要重新走一遍解锁表单。改为有密钥就直接 `decryptNoteContent` 回填，解不开（密钥属于别的密码）才回落到解锁表单并给出提示，切换笔记与重新解锁时清理 `revealFailed`。
- 搜索词长度上限只写在文档里没有实现：`GET /api/notes` 现在校验 `q` ≤ 200 字（`maxQueryRunes`），超限返回 400 + `fields.q`，前端两处搜索框（首页与列表页）同步 `maxlength="200"`，`writeValidationError` 的字段优先级纳入 `q`。
- `just stop` 在记录的 PID 已不存在时报 `NativeCommandError`：PowerShell 5.1 下 `$ErrorActionPreference='Stop'` 会把 taskkill 写到 stderr 的 "process not found" 升级为终止性错误（`2>$null` 也不能豁免），异常还导致 pid 文件残留、下次 stop 再次失败。`Stop-DevProcess` 改为先 `Get-Process` 判存活：已退出则只提示并清理 pid 文件；taskkill 调用局部放宽为 `Continue`，覆盖检查与击杀之间进程恰好退出的竞态。实测陈旧 pid 文件 stop 干净退出（exit 0）、真实 pid 杀树正常。
- Windows 构建输出改为 `note.exe`，避免无扩展名的二进制无法在 Nushell、PowerShell 中直接执行。

### Security

- 加密笔记的密钥派生与加解密全部在浏览器 WebCrypto 内完成：派生密钥 `extractable:false` 且只存在页面内存，密码、明文正文、派生密钥均不写 localStorage / sessionStorage / IndexedDB / Cookie，服务端日志与错误响应不含正文；锁定会话即丢弃密钥并清空内存草稿，无自动过期窗口。密文 AAD 绑定笔记 ID，跨笔记搬运密文会直接解密失败。
- Markdown 渲染只有一处 `{@html}`（`web/src/components/Markdown.svelte`），输入必须先过 `DOMPurify.sanitize`，外链自动补 `rel="noopener noreferrer"`；含正文与解密结果响应保持 `Cache-Control: no-store`。
- 主密码设置界面显式提示弱口令风险（建议 12 字符以上长口令）：密钥由口令派生，库中公盐与 `kdf_params` 公开，弱口令可被离线爆破。
- 默认只监听回环地址，绑定非回环地址必须显式 `--allow-remote` 并输出警告。
- 数据库文件权限收紧为 `0600`，启用 WAL、`secure_delete` 与 `quick_check`。
- 统一安全响应头、Host 白名单、CSRF Cookie，API 错误响应禁止缓存。
