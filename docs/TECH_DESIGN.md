# 个人笔记应用 技术方案

| 项目 | 内容 |
| --- | --- |
| 文档版本 | v1.0 |
| 日期 | 2026-10-26 |
| 状态 | 实施草案 |
| 关联文档 | [产品需求文档 v1.2](./PRD.md) |
| 目标 | 将 PRD 落地为可实现的 Go + SQLite + Svelte 5 技术方案 |

---

## 1. 目标与约束

### 1.1 实现目标

- 实现 PRD v1.2 的全部 MVP 范围：纯文本笔记 CRUD、列表分页、模糊查询、不可逆加密、密码提示、解锁会话、自动锁定。
- 运行时为单个 Go 可执行文件；前端在构建阶段编译并嵌入，不单独部署。
- 数据只保存在本地 SQLite 文件中，默认仅监听 `127.0.0.1`。
- 加密笔记在数据库中只保存密文；不保存密码、密码哈希或可用于重置的密钥。
- 页面由独立轻量 Svelte 5 SPA 提供，不使用 SvelteKit、UI 组件库、CSS 框架、路由库或状态管理库。
- 后端只提供 JSON API、静态资源和 SPA 回退，不进行服务端页面渲染。

### 1.2 不可妥协的安全不变量

1. 加密事务成功后，`notes.content` 必须为 `NULL`，密文写入 `notes.cipher`。
2. 密码只用于派生密钥，不写入数据库、日志、Cookie 或浏览器持久化存储。
3. 错误密码与密文损坏返回相同的外部错误，不泄露具体原因。
4. 解锁密钥只存在于进程内存；服务重启后全部失效。
5. 解密 API、包含正文的响应必须返回 `Cache-Control: no-store`。
6. 前端禁止 `{@html}`；标题、正文、提示问答只通过文本插值渲染。
7. 日志不包含正文、密码、提示答案、完整查询串、Cookie 或请求体。

### 1.3 非目标

多用户、云同步、分享、移动端原生应用、Markdown/富文本、附件、标签、回收站、密码找回/重置、修改密码、取消加密、加密正文搜索、公网认证与 TLS 终止。详见 PRD 第 2.2 节。

---

## 2. 总体架构

```text
┌──────────────────────────────────────────────┐
│ 浏览器                                       │
│ Svelte 5 SPA + TypeScript                    │
│ - 自写 History 路由                          │
│ - fetch /api，携带 X-CSRF-Token              │
│ - 不持久化密码、正文、解锁密钥               │
└──────────────────────┬───────────────────────┘
                       │ HTTP 127.0.0.1:8080
┌──────────────────────▼───────────────────────┐
│ Go 进程                                      │
│ net/http + ServeMux                          │
│ 配置: config/note.yaml（启动时加载、校验）    │
│                                              │
│ middleware: 恢复 / Host / 安全头 / 日志 / CSRF│
│                                              │
│ API handlers                                 │
│   ├── store    SQLite (data/notes.db, WAL)   │
│   ├── crypto   Argon2id + AES-256-GCM        │
│   └── session  仅内存解锁会话                │
│                                              │
│ 静态资源: //go:embed internal/web/dist       │
└──────────────────────┬───────────────────────┘
                       │
              data/notes.db (+ -wal/-shm)
```

### 2.1 请求路径

| 请求 | 处理 |
| --- | --- |
| `GET /assets/*` | 返回内嵌的 Vite 构建产物，带内容哈希的资源长期缓存 |
| `GET /api/*` | JSON API，动态响应 `no-store`；未知 API 返回 JSON 404 |
| `GET /`、`GET /notes/...` | 非 API 的 GET 回退到 `index.html`，由 Svelte 前端路由接管 |
| `POST/PUT/DELETE /api/*` | JSON 写请求，经过 Host、Origin、CSRF 校验 |

### 2.2 进程模型

- 单进程、单用户、无后台任务队列。
- SQLite 使用单连接（`SetMaxOpenConns(1)`），避免并发写与 PRAGMA 状态不一致；个人场景吞吐足够。
- Argon2id 计算通过容量为 1 的信号量串行化，避免并发解锁造成 64 MiB 级别内存叠加。
- KDF 计算不放在数据库事务内；数据库事务只覆盖短小的读写操作。
- 解锁会话保存在进程内，服务退出即清空。

### 2.3 目录结构

```text
note/
├── go.mod
├── cmd/note/main.go
├── config/
│   └── note.yaml                 # 默认运行配置（提交）
├── sql/
│   ├── embed.go                  # //go:embed *.sql
│   └── 0001_init.sql             # v1 表结构与索引
├── internal/
│   ├── config/config.go          # 加载、合并、校验 note.yaml
│   ├── store/
│   │   ├── store.go              # 启动检测、打开、PRAGMA、迁移
│   │   ├── notes.go              # CRUD、搜索
│   │   └── models.go
│   ├── crypto/
│   │   ├── crypto.go             # KDF、AEAD、参数校验
│   │   └── format.go             # 密文容器
│   ├── session/session.go        # 内存解锁会话
│   └── web/
│       ├── server.go             # 路由、静态资源、SPA 回退
│       ├── middleware.go         # 恢复、Host、安全头、日志、CSRF
│       ├── handlers_notes.go
│       ├── handlers_auth.go
│       ├── errors.go
│       ├── assets.go             # //go:embed all:dist
│       └── dist/                 # 前端构建产物（生成物，不提交）
├── web/
│   ├── src/
│   │   ├── api/
│   │   │   ├── client.ts
│   │   │   └── types.ts
│   │   ├── components/
│   │   ├── views/
│   │   ├── router/
│   │   ├── app.css
│   │   ├── App.svelte
│   │   └── main.ts
│   ├── index.html
│   ├── package.json
│   ├── package-lock.json
│   ├── svelte.config.js
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── vitest.config.ts
├── data/                         # 运行时生成，不提交
├── docs/
│   ├── PRD.md
│   └── TECH_DESIGN.md
├── justfile
├── README.md
└── LICENSE
```

---

## 3. 技术选型

| 层 | 选型 | 说明 |
| --- | --- | --- |
| 语言 | Go 1.22+ | 单二进制、标准库 HTTP 路由、部署简单 |
| HTTP | `net/http` + `ServeMux` | 使用 Go 1.22 方法路由，不引入 Web 框架 |
| 配置 | `gopkg.in/yaml.v3` | 解析 `config/note.yaml`，严格字段校验 |
| 数据库 | SQLite + `modernc.org/sqlite` | 纯 Go，无 CGO；本地单文件 |
| 密钥派生 | `golang.org/x/crypto/argon2` | Argon2id，内存困难型 KDF |
| 对称加密 | `crypto/aes` + `crypto/cipher` | AES-256-GCM 认证加密 |
| Unicode | `golang.org/x/text/unicode/norm` | 密码 NFC 规范化 |
| ID | `github.com/google/uuid` | UUID v4，避免自增 ID 泄露数量 |
| 前端 | Vite + Svelte 5 + TypeScript | 独立轻量 SPA，不使用 SvelteKit |
| 路由 | 自写 History 路由 | 仅 6 个视图，不引入路由库 |
| 样式 | Svelte scoped CSS + 全局 reset | 不引入 CSS 框架 |
| 前端测试 | Vitest + `@testing-library/svelte` | API 客户端、路由匹配、关键组件 |
| 类型检查 | `svelte-check` | 在构建和 CI 中执行 |
| 运行时 | Go 单二进制 | 前端产物通过 `//go:embed` 内嵌 |

前端运行时依赖为 0；Svelte 由 Vite 在构建阶段编译。Node/npm 只存在于开发和构建环境。

---

## 4. 后端设计

### 4.1 启动与关闭

启动顺序：

1. 解析命令行参数，加载 `config/note.yaml`，合并为最终配置并校验（见 4.2）。
2. 创建数据目录（POSIX `0700`）。
3. 打开 SQLite 连接，设置连接数与 PRAGMA。
4. 检测数据库状态：新建空库执行全量迁移；已有库做完整性检查并按需迁移（见 5.2）。
5. 校验内嵌前端是否存在 `index.html`。
6. 初始化解锁会话管理器与清理协程。
7. 装配路由与中间件，启动 HTTP 服务。
8. 监听 `SIGINT`/`SIGTERM`。

关闭顺序：

1. 收到信号后停止接收新请求。
2. 调用 `http.Server.Shutdown`（最多 10 秒）。
3. 停止会话清理协程，清空会话密钥。
4. 执行 `PRAGMA wal_checkpoint(TRUNCATE)`。
5. 关闭数据库连接。

启动失败时输出明确错误并以非零码退出；不得在数据库损坏时静默重建文件。

### 4.2 配置

配置来源优先级：内置默认值 < `config/note.yaml` < 命令行参数。不读取环境变量，避免配置隐式来源。

`config/note.yaml` 提交到仓库，作为默认配置与示例：

```yaml
server:
  addr: 127.0.0.1:8080
  allow_remote: false

database:
  path: ./data/notes.db
```

| 参数 | YAML 键 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `--config` | — | `./config/note.yaml` | 配置文件路径 |
| `--addr` | `server.addr` | `127.0.0.1:8080` | 监听地址 |
| `--db` | `database.path` | `./data/notes.db` | SQLite 文件路径 |
| `--allow-remote` | `server.allow_remote` | `false` | 允许绑定非回环地址；启动时打印安全警告 |

规则：

- YAML 使用严格解码：未知键、类型错误、非法取值都拒绝启动，并输出具体字段。
- `--config` 显式指定但文件不存在或无法读取 → 拒绝启动；使用默认路径且文件缺失 → 使用内置默认值并打印警告。
- 命令行参数只覆盖显式提供的项，未提供的项沿用 YAML 或内置默认值。
- 相对路径基于进程工作目录解析。
- 未启用 `allow_remote` 时，`addr` 必须是回环地址；否则拒绝启动。
- 启动日志记录最终生效的监听地址、数据库路径与 `allow_remote`，不打印其他配置内容。
- 数据目录不存在时自动创建，权限 `0700`。
- 数据库文件创建后，在 POSIX 上尽力设置为 `0600`。

### 4.3 路由与中间件

```text
GET    /api/csrf
GET    /api/notes
POST   /api/notes
GET    /api/notes/{id}
PUT    /api/notes/{id}
DELETE /api/notes/{id}
POST   /api/notes/{id}/encrypt
POST   /api/notes/{id}/unlock
POST   /api/notes/{id}/lock
POST   /api/lock
GET    /assets/*
GET    /*                     # 非 API GET → index.html
```

中间件顺序：

1. panic 恢复：记录不含敏感数据的堆栈摘要，返回 500。
2. Host 校验：只允许配置主机及回环别名，失败返回 400。
3. 安全响应头：CSP、`nosniff`、`DENY`、`Referrer-Policy`、缓存策略。
4. 访问日志：方法、路由模式/路径、状态码、耗时、响应字节数；不记录查询串与请求体。
5. CSRF：仅对 `POST`/`PUT`/`DELETE` 校验 `X-CSRF-Token`、Cookie、Origin/Sec-Fetch-Site。
6. 业务处理器。

静态与回退规则：

- `/assets/*` 由内嵌文件系统提供；缺失资源返回 404，不回退到 `index.html`。
- `/api/*` 未知路径返回 JSON 404，不回退到 `index.html`。
- 其余 GET 请求满足以下任一条件时返回 `index.html`：
  - `Accept` 包含 `text/html`；
  - 路径最后一段不含 `.`。
- `index.html` 返回 `Cache-Control: no-store`；带哈希的 `/assets/*` 返回 `public, max-age=31536000, immutable`。

### 4.4 处理器职责与校验

| 处理器 | 职责 | 关键校验 |
| --- | --- | --- |
| `GET /api/csrf` | 签发/复用 CSRF Token | 无 |
| `GET /api/notes` | 列表与搜索 | `q` ≤ 200 字符；`page` ≥ 1；页大小固定 20 |
| `POST /api/notes` | 创建明文或加密笔记 | 标题、正文、密码、确认密码、提示、确认项 |
| `GET /api/notes/{id}` | 详情；加密未解锁只返回元数据 | UUID 合法；否则 404 |
| `PUT /api/notes/{id}` | 更新标题/正文；加密笔记需解锁 | 标题、正文；会话密钥存在 |
| `DELETE /api/notes/{id}` | 永久删除，不要求密码 | UUID 合法 |
| `POST /api/notes/{id}/encrypt` | 明文笔记转为加密 | 笔记为明文；密码、确认项 |
| `POST /api/notes/{id}/unlock` | 校验密码并建立解锁会话 | 笔记为加密；密码非空 |
| `POST /api/notes/{id}/lock` | 删除单条笔记密钥 | 幂等 |
| `POST /api/lock` | 删除整个会话 | 幂等 |

通用校验：

- 写请求 `Content-Type` 必须是 `application/json`（允许带 charset）；DELETE 允许空请求体。
- JSON 解码使用 `DisallowUnknownFields`；请求体上限 2 MiB。
- 标题：去首尾空白后 1–200 个字符，不允许 U+0000。
- 正文：UTF-8 纯文本，最大 1 MiB，不允许 U+0000；允许空字符串。
- 密码：NFC 规范化后 8–256 个字符，UTF-8 编码不超过 1024 字节；不去除空格。
- 密码确认必须一致。
- 提示问答：最多 3 组；问题 1–100 字符、答案 1–200 字符；两项必须同时存在或同时为空。
- 加密请求必须携带 `acknowledge_irreversible: true`。
- 创建明文笔记时不得携带密码、提示或确认项；创建加密笔记时必须携带。
- 更新加密笔记时，正文按新明文重新加密；标题仍为明文。
- 所有错误信息不得包含 SQL、文件路径、密钥、密码或密文内容。

### 4.5 错误模型

统一响应：

```json
{
  "error": {
    "code": "validation_failed",
    "message": "标题不能为空",
    "fields": {
      "title": "标题不能为空"
    }
  }
}
```

| code | HTTP | 说明 |
| --- | --- | --- |
| `validation_failed` | 400 | 字段校验失败 |
| `bad_request` | 400 | JSON 解析失败或结构错误 |
| `host_invalid` | 400 | Host 不在白名单 |
| `unlock_required` | 401 | 未解锁或解锁会话过期 |
| `decrypt_failed` | 401 | 密码错误、密文损坏或 KDF 参数异常 |
| `csrf_invalid` | 403 | CSRF Token / Origin 校验失败 |
| `not_found` | 404 | 笔记或 API 不存在 |
| `method_not_allowed` | 405 | 方法不允许 |
| `already_encrypted` | 409 | 对加密笔记再次加密 |
| `not_encrypted` | 409 | 对明文笔记执行解锁 |
| `conflict` | 409 | 并发修改导致加密条件不满足 |
| `payload_too_large` | 413 | 请求体超过限制 |
| `unsupported_media_type` | 415 | 写请求不是 JSON |
| `internal` | 500 | 内部错误，不返回细节 |

`decrypt_failed` 的 `message` 固定为「密码错误或数据损坏」，不区分具体原因。KDF 参数越界、GCM 认证失败、密文长度异常都映射到该错误；服务端可记录内部类别，但不得回传。

### 4.6 日志

- 输出到 stdout，单行结构化文本或 JSON。
- 访问日志字段：时间、级别、方法、路由模式、状态码、耗时、响应字节数。
- 禁止记录：请求体、密码、密码确认、提示答案、正文、完整查询串、Cookie、Authorization。
- 加密/解密失败记录笔记 ID 与类别（如 `decrypt_failed`、`kdf_params_invalid`），不记录输入。
- 启动日志可记录数据库路径、监听地址、是否启用远程绑定；不记录密钥或密码。

### 4.7 并发、超时与限制

- HTTP：`ReadHeaderTimeout=5s`、`ReadTimeout=15s`、`WriteTimeout=30s`、`IdleTimeout=60s`。
- 请求体：`http.MaxBytesReader` 限制为 2 MiB；正文自身限制 1 MiB。
- KDF：容量为 1 的信号量，获取失败时等待，不返回并发错误。
- 数据库：单连接，短事务；KDF 不在事务内执行。
- 密码错误不做账户锁定；本地攻击者可离线读取数据库，应用层限速无法提供实质保护。Argon2id 成本与单 KDF 并发限制是主要防线。

---

## 5. 数据层

### 5.1 Schema

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA secure_delete = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS notes (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    content      TEXT,
    is_encrypted INTEGER NOT NULL DEFAULT 0 CHECK (is_encrypted IN (0, 1)),
    cipher       BLOB,
    kdf_salt     BLOB,
    kdf_params   TEXT,
    hints        TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    CHECK (
        (is_encrypted = 0
            AND content IS NOT NULL
            AND cipher IS NULL
            AND kdf_salt IS NULL
            AND kdf_params IS NULL
            AND hints IS NULL)
        OR
        (is_encrypted = 1
            AND content IS NULL
            AND cipher IS NOT NULL
            AND kdf_salt IS NOT NULL
            AND kdf_params IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_notes_updated_at
    ON notes(updated_at DESC);
```

以上 DDL 的源文件为 `sql/0001_init.sql`；修改表结构时同步更新文档、迁移文件与 `user_version`。

字段约定：

- `id`：UUID v4 字符串。
- `title`：明文，用于列表与搜索。
- `content`：明文正文；加密后必须为 `NULL`。
- `cipher`：`nonce(12) || ciphertext || tag(16)`。
- `kdf_salt`：16 字节随机盐。
- `kdf_params`：JSON，见 6.1。
- `hints`：JSON 数组 `[{"question":"...","answer":"..."}]`；无提示为 `NULL`。提示明文保存。
- 时间：UTC，固定格式 `2006-01-02T15:04:05Z`（秒级），便于字符串排序；相同时间以 `id DESC` 兜底。
- `hints` 解析失败时记录告警并按空提示处理，不影响解锁；`kdf_params` 非法时解密失败。

### 5.2 启动检测、连接与 PRAGMA

启动时按固定顺序处理数据库；任一步失败都终止启动并保留原文件，不自动删除或重建：

1. 确认数据目录存在，不存在则创建（POSIX `0700`）。
2. 记录数据库文件是否存在；打开连接并设置连接数与 PRAGMA（如下）。文件不存在时由 SQLite 创建空库。
3. 检测数据库状态：
   - 本次启动新建了空库 → 执行全部迁移，记录 `database initialized`；
   - 已有文件 → 执行 `PRAGMA quick_check`，失败则拒绝启动；
   - `user_version` 高于当前程序支持的版本 → 拒绝启动，避免旧程序写坏新结构。
4. 执行 `user_version` 之后待应用的迁移（见 5.3），成功后记录 schema 版本。
5. 以上步骤完成后才启动 HTTP 服务，页面可用时数据库已就绪。

连接与 PRAGMA：

- DSN 示例：
  `file:data/notes.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=secure_delete(ON)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)`
- `db.SetMaxOpenConns(1)`、`db.SetMaxIdleConns(1)`。
- 单连接让 PRAGMA、事务和关闭顺序确定；代价是查询串行化，本项目可接受。
- 数据库路径指向目录、权限不足或文件不是有效 SQLite 库时，输出明确错误并以非零码退出。
- 数据库文件权限在 POSIX 上设为 `0600`，失败时记录告警但不阻止启动。

### 5.3 迁移

- 表结构定义集中在仓库根目录 `sql/`：
  - `0001_init.sql`：v1 的 `notes` 表与索引，内容与 5.1 一致；
  - `embed.go`：用 `//go:embed *.sql` 暴露嵌入文件系统，供 `internal/store` 读取。
- 文件名前缀为版本号；启动时按版本升序执行，已执行版本由 `PRAGMA user_version` 记录。
- 每个版本的迁移在单个事务内执行；任一语句失败则回滚并拒绝启动，成功后更新 `user_version`。
- 新建数据库执行全部迁移；已有数据库只执行 `user_version` 之后的版本。
- 迁移只向前；不自动删除列、不重建表。
- 升级前建议用户备份数据库；不自动执行 `VACUUM`，避免大库启动耗时。

### 5.4 查询与搜索

列表/搜索 SQL（伪代码）：

```sql
-- :pattern = "%" || escaped(q) || "%"
SELECT id, title, is_encrypted,
       CASE WHEN is_encrypted = 0 THEN substr(content, 1, 140) END AS snippet,
       created_at, updated_at
  FROM notes
 WHERE (:q = '' OR title LIKE :pattern ESCAPE '\'
        OR (is_encrypted = 0 AND content LIKE :pattern ESCAPE '\'))
 ORDER BY
   CASE WHEN :q <> '' AND title LIKE :pattern ESCAPE '\' THEN 0 ELSE 1 END,
   updated_at DESC,
   id DESC
 LIMIT 20 OFFSET :offset;
```

- 转义顺序：`\` → `\\`，`%` → `\%`，`_` → `\_`。
- `q` 去首尾空白；为空时返回最近更新列表。
- `page` 最小为 1；页大小固定 20；总数用同条件 `COUNT(*)`。
- `snippet` 仅对明文笔记生成，换行折叠为空格；加密笔记为 `NULL`。
- SQLite `LIKE` 对 ASCII 大小写不敏感，对中文为精确子串；MVP 不做分词、拼音和纠错。
- 查询词通过 URL 参数传递，会进入浏览器历史；服务端日志不记录查询串。
- 数据量增长后评估 FTS5 `trigram`，仅索引明文笔记；MVP 不引入。

### 5.5 备份、删除与残留

- 备份对象：`notes.db`、`notes.db-wal`、`notes.db-shm`；推荐停止应用或使用 `sqlite3 notes.db ".backup 'backup.db'"`。
- 删除为永久删除；MVP 不提供回收站。
- 全局 `secure_delete=ON`；删除/加密事务提交后执行 `PRAGMA wal_checkpoint(TRUNCATE)`。
- 应用退出时再次 checkpoint。
- `VACUUM` 作为可选维护操作；MVP 不自动执行。
- 边界：旧备份、文件系统快照、SSD 磨损均衡层可能保留旧明文；应用只保证运行时数据库逻辑删除与 WAL 清理。

---

## 6. 加密设计

### 6.1 参数

| 项目 | 值 |
| --- | --- |
| KDF | Argon2id，version 19 |
| 内存 | 64 MiB（65536 KiB） |
| 迭代次数 | 3 |
| 并行度 | 2 |
| 派生密钥长度 | 32 字节 |
| 盐 | 16 字节随机值，每条笔记每次加密生成 |
| AEAD | AES-256-GCM，12 字节随机 nonce |
| AAD | `note:v1:` + 笔记 UUID |
| 密文容器 | `nonce || ciphertext || tag` |
| 密码规范化 | NFC |

`kdf_params` JSON：

```json
{"algo":"argon2id","version":19,"time":3,"memory":65536,"threads":2,"keylen":32}
```

读取时校验：

- `algo == "argon2id"`
- `version == 19`
- `time ∈ [1, 10]`
- `memory ∈ [8192, 262144]` KiB
- `threads ∈ [1, 8]`
- `keylen == 32`

越界视为数据损坏，拒绝解密。

### 6.2 数据格式

```text
kdf_salt  = 16 字节随机值
kdf_params = 上述 JSON
cipher    = nonce[12] || AES-256-GCM(plaintext, aad)
aad       = []byte("note:v1:" + noteID)
```

- `noteID` 为 UUID 字符串；格式版本参与 AAD，防止密文跨笔记移植。
- 同一笔记每次加密/重加密使用新的 12 字节 nonce；盐在设置密码时生成，编辑重加密沿用（编辑时只持有派生后的密钥，无法重新派生）。
- 不使用固定 nonce；随机 nonce 由 `crypto/rand` 生成。

### 6.3 密钥派生

```text
normalized = NFC(password)
key = Argon2id(normalized, salt,
               time=3, memory=65536, threads=2, keylen=32)
```

- 规范化在长度校验前执行；长度按规范化后的字符数计。
- 不保存密码、密码哈希或独立校验值。
- 密码是否正确由 GCM 认证标签判断。
- KDF 计算在数据库事务外执行，并通过信号量限制并发为 1。

### 6.4 加密流程

#### 新建加密笔记

1. 校验标题、正文、密码、确认项、提示。
2. 生成 UUID、盐、nonce；派生密钥并加密正文。
3. 单个事务插入记录：`content=NULL`、`cipher`、`kdf_salt`、`kdf_params`、`hints`、`is_encrypted=1`。
4. 提交后 checkpoint，清零内存密钥。
5. 返回 201，详情为未解锁状态（不自动建立会话）。

明文正文从未写入数据库。

#### 明文笔记转为加密

1. 读取笔记，确认 `is_encrypted=0`，记录 `updated_at` 与正文。
2. 派生密钥、生成 nonce、加密正文。
3. 事务执行条件更新：

```sql
UPDATE notes
   SET cipher = ?, kdf_salt = ?, kdf_params = ?, hints = ?,
       content = NULL, is_encrypted = 1, updated_at = ?
 WHERE id = ? AND is_encrypted = 0 AND updated_at = ?;
```

4. 若影响行数为 0，返回 409 `conflict`（并发生效或已加密）。
5. 提交后 checkpoint，清零密钥。
6. 返回 200，详情为未解锁状态。

KDF 不占用数据库事务；乐观条件避免把过期明文加密覆盖新编辑。

### 6.5 解密流程

1. 读取 `cipher`、`kdf_salt`、`kdf_params`。
2. 校验参数边界，派生密钥。
3. 拆分 nonce，调用 `gcm.Open`，AAD 绑定笔记 ID。
4. 成功：返回明文；解锁时把密钥放入会话。
5. 失败：清零密钥，返回统一的 `decrypt_failed`。

密码错误、密文篡改、nonce 损坏、AAD 不匹配、参数越界都使用同一外部错误。

### 6.6 编辑重加密

- 仅在有效解锁会话下允许。
- 使用会话中的 32 字节密钥，生成新的 12 字节 nonce，重新加密新正文。
- 盐和 KDF 参数不变；不修改密码。
- 事务更新 `cipher`、`title`、`updated_at`；`content` 保持 `NULL`。
- 保存后不保留明文字节切片的引用；尽力清零（Go GC 不保证彻底擦除）。
- 若会话过期，返回 401 `unlock_required`，前端跳转解锁视图。

### 6.7 错误与内存

- `internal/crypto` 定义 `ErrDecrypt`；上层只暴露 `decrypt_failed`。
- 派生密钥、明文切片使用后清零；Go 无法保证所有副本被擦除，文档不得夸大该保证。
- 不把明文写入日志、错误消息、panic 信息或调试输出。
- KDF 使用信号量，避免并发大内存分配。
- 加密失败不修改数据库；事务提交前发生任何错误都保持原状态。

### 6.8 明文残留

- `secure_delete=ON` 让 SQLite 覆盖删除页。
- 加密、删除事务后执行 `PRAGMA wal_checkpoint(TRUNCATE)`。
- 退出时再次 checkpoint。
- 旧备份、快照、SSD 层不在应用控制范围；文档提示用户自行保护。
- 验收时对主库与 WAL 执行标记文本检索，确保运行时数据库无明文残留。

---

## 7. 解锁会话与 CSRF

### 7.1 解锁会话

```text
SessionManager
  mutex
  sessions: map[token]*Session

Session
  keys: map[noteID][]byte
  expiresAt: time.Time
```

- Token：32 字节随机值，十六进制编码；Cookie 名 `note_sess`。
- Cookie：`HttpOnly`、`SameSite=Strict`、`Path=/`；本地 HTTP 不设 `Secure`，经 TLS 时应启用。
- Cookie 为浏览器会话级（不设 `Max-Age`）；关闭浏览器后自然失效，服务端 TTL 仍生效。
- TTL：10 分钟空闲过期；每次成功读取该笔记正文时刷新。
- 多笔记：一个会话内保存多个笔记密钥，支持同时解锁多条。
- 单条锁定：删除对应密钥；若会话无剩余密钥，删除会话并清 Cookie。
- 全部锁定：删除整个会话，清 Cookie。
- 清理：后台每分钟清理过期会话。
- 重启/关闭：全部密钥清零；Cookie 残留也无对应服务端会话。
- 锁定只影响后续请求；已经渲染在屏幕上的内容无法远程擦除。

### 7.2 CSRF

- `GET /api/csrf`：
  - 若 Cookie `csrf` 已存在且格式合法，复用并返回；
  - 否则生成 32 字节随机 Token，设置为 `HttpOnly`、`SameSite=Strict`、`Path=/` 的 Cookie，并返回 JSON。
- 写请求必须携带 `X-CSRF-Token`，服务端以常量时间比较请求头与 Cookie。
- 同时校验：
  - `Origin` 存在时必须与请求 Host 同源；
  - `Sec-Fetch-Site` 存在时必须是 `same-origin`；
  - 两者都缺失时仍要求 CSRF Token（Token 无法被跨站读取）。
- 开发模式下 Vite 代理需把 `Origin` 重写为 Go 的目标源（见 10.1），否则浏览器发送的 `http://localhost:5173` 与代理后的 Host 不一致，写请求会被拒绝；生产环境由 Go 同源提供，无需重写。
- 写请求强制 JSON Content-Type，进一步降低浏览器原生表单跨站提交面。
- 前端在内存缓存 Token；遇到 403 `csrf_invalid` 时重新获取并重试一次。
- Token Cookie 为会话级 Cookie；清除 Cookie 后重新获取。

### 7.3 Host 校验

- 默认允许：`127.0.0.1`、`localhost`、`[::1]` 及配置端口。
- 显式 `--allow-remote` 时，额外允许配置的监听主机。
- 规范化 Host（含默认端口）后比较，失败返回 400 `host_invalid`。
- Vite 开发代理使用 `changeOrigin: true`，确保转发后的 Host 为 Go 监听地址。

---

## 8. API 设计

### 8.1 约定

- 前缀：`/api`。
- 请求/响应：`application/json; charset=utf-8`。
- 写请求：JSON 请求体，CSRF 头；成功不返回 303，导航由前端完成。
- 所有 API 响应 `Cache-Control: no-store`（204 除外无响应体）。
- 不启用 CORS，不返回 `Access-Control-Allow-*`。
- 错误结构见 4.5。
- 时间字段为 UTC RFC3339 字符串，前端按本地时区展示。
- 列表页大小固定 20。

### 8.2 数据结构

**NoteSummary**

```json
{
  "id": "5b0e...",
  "title": "会议记录",
  "is_encrypted": false,
  "snippet": "第一行内容...",
  "created_at": "2026-10-26T02:00:00Z",
  "updated_at": "2026-10-26T02:30:00Z"
}
```

**NoteDetail**

```json
{
  "id": "5b0e...",
  "title": "私密记录",
  "is_encrypted": true,
  "unlocked": false,
  "content": null,
  "hints": [
    {"question": "我最喜欢的城市？", "answer": "第一次独自旅行去的地方"}
  ],
  "created_at": "2026-10-26T02:00:00Z",
  "updated_at": "2026-10-26T02:30:00Z"
}
```

- 明文笔记：`is_encrypted=false`、`unlocked=true`、`content` 有值、`hints=null`。
- 加密未解锁：`content=null`、`unlocked=false`、`hints` 可能为 `null`。
- 加密已解锁：`content` 有值、`unlocked=true`。
- 正文、标题、提示均为纯文本；不返回 HTML。

**ListResponse**

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "total": 0,
  "total_pages": 0,
  "query": ""
}
```

### 8.3 端点

| 方法 | 路径 | 请求 | 成功响应 |
| --- | --- | --- | --- |
| GET | `/api/csrf` | 无 | 200 `{"token":"..."}` + Set-Cookie |
| GET | `/api/notes?q=&page=` | 查询参数 | 200 ListResponse |
| POST | `/api/notes` | 创建请求 | 201 NoteDetail（加密笔记为未解锁） |
| GET | `/api/notes/{id}` | 无 | 200 NoteDetail |
| PUT | `/api/notes/{id}` | 更新请求 | 200 NoteDetail |
| DELETE | `/api/notes/{id}` | 无 | 204 |
| POST | `/api/notes/{id}/encrypt` | 加密请求 | 200 NoteDetail（未解锁） |
| POST | `/api/notes/{id}/unlock` | 解锁请求 | 200 `{"content":"...","expires_at":"..."}` + Set-Cookie |
| POST | `/api/notes/{id}/lock` | 无 | 200 `{"locked":true}` |
| POST | `/api/lock` | 无 | 200 `{"locked":true}` |

### 8.4 请求示例

**创建明文笔记**

```json
{
  "title": "购物清单",
  "content": "牛奶\n鸡蛋",
  "encrypt": false
}
```

**创建加密笔记**

```json
{
  "title": "私密记录",
  "content": "正文",
  "encrypt": true,
  "password": "correct horse battery staple",
  "password_confirm": "correct horse battery staple",
  "hints": [
    {"question": "我最喜欢的城市？", "answer": "第一次独自旅行去的地方"}
  ],
  "acknowledge_irreversible": true
}
```

**更新笔记**

```json
{
  "title": "购物清单",
  "content": "牛奶\n鸡蛋\n面包"
}
```

- 明文笔记直接更新。
- 加密笔记必须有 `note_sess` 会话；服务端用会话密钥重新加密正文。
- 无会话时返回 401 `unlock_required`。

**加密现有笔记**

```json
{
  "password": "new-note-password",
  "password_confirm": "new-note-password",
  "hints": [],
  "acknowledge_irreversible": true
}
```

**解锁**

```json
{
  "password": "correct horse battery staple"
}
```

成功响应：

```json
{
  "content": "正文",
  "expires_at": "2026-10-26T03:00:00Z"
}
```

**CSRF**

```http
GET /api/csrf
```

```json
{"token":"base64url-token"}
```

### 8.5 校验与状态映射

| 场景 | 状态 |
| --- | --- |
| 创建成功 | 201 |
| 读取/更新/加密/解锁/锁定成功 | 200 |
| 删除成功 | 204 |
| 字段错误 | 400 |
| 未解锁/会话过期 | 401 `unlock_required` |
| 密码错误/密文损坏 | 401 `decrypt_failed` |
| CSRF/Origin 失败 | 403 |
| Host 失败 | 400 |
| 未找到 | 404 |
| 方法不允许 | 405 |
| 已加密再次加密 | 409 `already_encrypted` |
| 明文笔记执行解锁 | 409 `not_encrypted` |
| 加密并发冲突 | 409 `conflict` |
| 请求体过大 | 413 |
| 写请求非 JSON | 415 |
| 内部错误 | 500 |

---

## 9. 前端设计

### 9.1 技术栈与脚本

- Vite + Svelte 5 + TypeScript；不使用 SvelteKit。
- 路由、状态、样式均使用 Svelte 内置能力；不引入 UI、CSS、路由、状态库。
- TypeScript 开启 `strict`。
- `package.json` 目标脚本：

```json
{
  "scripts": {
    "dev": "vite",
    "build": "svelte-check --tsconfig ./tsconfig.json && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "check": "svelte-check --tsconfig ./tsconfig.json"
  }
}
```

- 运行时依赖为 0；Svelte 作为构建依赖编译进产物。

### 9.2 目录与组件

```text
web/src/
├── api/
│   ├── client.ts        # fetch、CSRF、错误映射
│   └── types.ts         # NoteSummary/NoteDetail/Error
├── components/
│   ├── Layout.svelte
│   ├── SearchBar.svelte
│   ├── NoteListItem.svelte
│   ├── Pagination.svelte
│   ├── UnlockForm.svelte
│   ├── HintList.svelte
│   ├── PasswordInput.svelte
│   ├── ErrorBanner.svelte
│   └── EmptyState.svelte
├── views/
│   ├── NoteListView.svelte
│   ├── NoteDetailView.svelte
│   ├── NoteEditorView.svelte
│   ├── EncryptView.svelte
│   ├── ForgotPasswordView.svelte
│   └── NotFoundView.svelte
├── router/
│   └── router.ts        # 自写 History 路由
├── app.css
├── App.svelte
└── main.ts
```

### 9.3 路由

- 路由表：

| 路径 | 视图 |
| --- | --- |
| `/` | 列表/搜索 |
| `/notes/new` | 新建 |
| `/notes/:id` | 详情/解锁 |
| `/notes/:id/edit` | 编辑 |
| `/notes/:id/encrypt` | 加密设置 |
| `/notes/:id/forgot` | 忘记密码 |
| 其他 | 404 |

- 使用 History API：`pushState`、`replaceState`、`popstate`。
- `router.ts` 暴露 `route` store（Svelte `writable`）、`navigate(path, replace?)`、`startRouter()`、`stopRouter()`。
- 路径匹配解析 `id`；查询参数由页面通过 `URLSearchParams` 解析。
- 内部链接统一通过拦截点击调用 `navigate`，保留浏览器前进/后退。
- 路由切换后滚动到顶部、更新 `document.title`、将焦点移到主标题。
- 未知路径显示前端 404 视图；Go 只负责回退 `index.html`。

### 9.4 API 客户端

- `src/api/client.ts` 统一封装：

```text
request<T>(path, init)
  ├── GET: cache: "no-store", credentials: "same-origin"
  ├── 写请求: 确保 CSRF Token，附加 X-CSRF-Token
  ├── 解析 JSON 错误为 ApiError { status, code, message, fields }
  └── 403 csrf_invalid: 重新获取 Token，重试一次
```

- 写请求设置 `Content-Type: application/json`。
- 视图传入 `AbortController`，离开页面时取消请求。
- 401 处理由调用方决定：
  - `unlock_required`：跳转该笔记详情/解锁视图；
  - `decrypt_failed`：在解锁表单显示统一错误。
- 不在客户端缓存笔记正文；请求返回后仅存在于当前视图状态。

### 9.5 状态与数据流

- 全局状态仅包含：当前路由、短暂 flash 消息。
- 笔记列表、详情、表单、解锁状态均为视图局部状态。
- 不使用 localStorage、sessionStorage、IndexedDB 保存密码、正文或解锁密钥。
- 页面刷新后，若服务端解锁会话仍有效，可重新获取已解锁正文；否则显示解锁视图。
- 用户点击锁定时，调用 API，清空当前视图正文，导航回详情/解锁视图。
- 列表不做长期缓存；每次进入列表按当前查询重新请求。

### 9.6 页面行为

| 视图 | 行为 |
| --- | --- |
| 列表/搜索 | 读取 `q`/`page`；请求列表；空状态、错误、加载；新建入口 |
| 详情 | 请求详情；明文显示正文与操作；加密未解锁显示密码与提示；已解锁显示正文、编辑、加密、删除、锁定 |
| 新建/编辑 | 表单校验；保存后导航详情；编辑加密笔记需已解锁；离开前未保存提醒 |
| 加密设置 | 密码、确认密码、提示、不可逆确认；提交后导航详情；已加密则跳转详情 |
| 忘记密码 | 展示提示、不可恢复说明、删除入口；删除需确认，不要求密码 |
| 404 | 返回列表链接 |

### 9.7 前端安全约束

- 禁止 `{@html}`；所有用户内容通过 `{...}` 文本插值。
- 密码输入框：
  - 解锁使用 `autocomplete="current-password"`；
  - 设置密码使用 `autocomplete="new-password"`；
  - 不使用 `value` 预填密码；提交后清空内存状态。
- 不把密码、正文、提示答案写入 URL、日志、分析埋点或浏览器存储。
- 不使用外部 CDN、字体、图片或脚本。
- API 响应 `no-store`；前端 `fetch` 对 GET 使用 `cache: "no-store"`。
- 删除使用明确的二次确认；加密确认项不可默认勾选。

### 9.8 样式与可访问性

- `app.css` 提供 reset、CSS 变量、布局、表单、按钮、状态样式；组件使用 Svelte scoped CSS。
- 移动优先，≥360px 可用；桌面最大宽度约 880px。
- 语义化 HTML，`label` 关联输入，错误使用 `aria-live`/`aria-invalid`。
- 路由切换后焦点移动到主标题；模态确认保持焦点在控件内。
- 不依赖颜色表达唯一状态；保证键盘可完成主流程。

### 9.9 开发与生产

- 开发：Vite `5173`，`/api` 代理到 `127.0.0.1:8080`，`changeOrigin: true`。
- 生产：Vite 构建到 `internal/web/dist`，Go 同源提供页面、资源和 API。
- 首次开发或拉取新代码后需先构建一次 `dist`，否则 Go 的 `//go:embed` 编译失败；之后前端改动由 Vite 开发服务器负责。

---

## 10. 构建与发布

### 10.1 Vite 与 Svelte 配置

`web/vite.config.ts`：

```ts
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        configure(proxy) {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('origin', 'http://127.0.0.1:8080')
          })
        },
      },
    },
  },
})
```

- `changeOrigin` 让转发后的 Host 与 Go 监听地址一致。
- 开发代理需要同时重写 `Origin`，否则 Go 的 Origin 同源校验会拒绝写请求；生产环境同源，无需该配置。
- 若后端端口改变，代理目标与 Origin 重写值需同步修改。

`web/svelte.config.js`：

```js
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte'

export default {
  preprocess: vitePreprocess(),
}
```

### 10.2 Go 嵌入与静态服务

```go
//go:embed all:dist
var embeddedDist embed.FS

func distFS() fs.FS {
    sub, err := fs.Sub(embeddedDist, "dist")
    if err != nil {
        panic(err)
    }
    return sub
}
```

- 构建前必须存在 `internal/web/dist/index.html`。
- 使用 `http.FileServer` 或自定义文件处理器提供 `/assets/*`；禁止目录列表。
- 设置 `/assets/*` 长期缓存；`index.html` 不缓存。
- SPA 回退只处理非 API、非资源 GET 请求。

### 10.3 justfile 目标

```make
frontend:
    cd web && npm ci && npm run build

build: frontend
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o note ./cmd/note

test:
    go test ./...
    cd web && npm test

check:
    cd web && npm run check
```

- 发布统一使用 `just build`：前端构建 → Go 编译。
- `internal/web/dist` 为生成物，不提交；缺失时 `go build` 因 embed 失败而中止。
- 目标机器只需二进制与数据目录，不需要 Node/npm。

### 10.4 发布检查

1. 干净目录执行 `npm ci` 与前端构建。
2. `go test ./...` 与 `go test -race ./...`。
3. `just build` 产出单二进制。
4. 将二进制复制到未安装 Node 的机器，验证页面、深链接、API。
5. 在空数据目录首次启动，确认自动建库与迁移；修改 `config/note.yaml` 与命令行覆盖的优先级符合 4.2。
6. 执行 PRD 第 9 节验收项，特别是明文残留检查。
7. 检查 `svelte-check`、前端测试与 `npm audit`（按需）。

### 10.5 构建可复现性

- 提交 `package-lock.json`；使用 `npm ci`。
- 固定 Node 22 LTS 及以上（建议 24 LTS），通过 `.nvmrc`/`engines` 约束。
- 不提交 `node_modules` 与 `internal/web/dist`。
- 建议在 CI 中从干净检出执行完整构建与测试。

---

## 11. 安全设计

### 11.1 威胁模型摘要

| 威胁 | 防护 | 说明 |
| --- | --- | --- |
| 数据库文件被复制 | 部分 | 正文密文；标题、提示、时间明文；弱密码可离线爆破 |
| 磁盘离线读取 | 部分 | 依赖密码强度与 Argon2id |
| 同机他人操作应用 | 否 | MVP 无应用登录，依赖系统账户 |
| 键盘记录/截屏/内存 | 否 | 本地明文工具无法防御 |
| 跨站写请求 | 是 | CSRF + Origin + JSON Content-Type |
| DNS Rebinding | 是 | Host 白名单 + 回环监听 |
| 存储型 XSS | 是 | Svelte 转义、禁止 `{@html}`、CSP |
| SSD 物理残留 | 否 | 应用只做逻辑清理与 WAL checkpoint |

### 11.2 响应头

```text
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self';
  img-src 'self' data:; font-src 'self'; connect-src 'self';
  form-action 'self'; frame-ancestors 'none'; base-uri 'none'
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
```

缓存：

- API：`Cache-Control: no-store`。
- `index.html`：`no-store`。
- `/assets/*`：`public, max-age=31536000, immutable`。

### 11.3 CSRF 与 Host

- CSRF Token 与 Cookie 双提交；Token 通过 JSON 返回，Cookie 为 HttpOnly。
- 写请求同时校验 Origin/Sec-Fetch-Site、JSON Content-Type。
- Host 白名单见 7.3；非回环绑定需显式开关。
- 不启用 CORS。

### 11.4 文件权限

- 数据目录 `0700`，数据库文件 `0600`（POSIX）。
- 不把数据库放在同步盘或公开目录；文档提示用户。
- 备份文件同样按敏感数据处理。

### 11.5 日志与错误

- 日志不包含正文、密码、提示答案、查询串、请求体、Cookie。
- 外部错误统一文案，不返回 SQL、路径、密钥或密文。
- 内部错误只记录类别与笔记 ID。
- panic 恢复不输出敏感堆栈内容到响应。

### 11.6 限制

- 无应用级认证：任何能访问本机端口的人都能操作应用。
- 加密仅保护静态数据库副本；已解锁页面可被截屏或内存抓取。
- 自动锁定只影响后续请求，不擦除已显示的页面。
- 公网暴露需要额外的 TLS 与认证层，MVP 不提供。

---

## 12. 测试方案

### 12.1 Go 单元与集成测试

| 包 | 覆盖 |
| --- | --- |
| `config` | 内置默认值、YAML 解析、命令行覆盖、未知键与非法值拒绝、相对路径解析 |
| `crypto` | 往返、错误密码、篡改 nonce/密文/tag/AAD、跨笔记 AAD、KDF 参数边界、NFC、固定随机源 golden test |
| `store` | 空库自动建表、已有库按版本迁移、`user_version` 高于程序时拒绝启动、迁移幂等、CHECK 约束、CRUD、搜索转义、分页、加密字段互斥、乐观加密冲突 |
| `session` | 解锁、查找、刷新、过期、单条锁定、全部锁定、并发（`-race`） |
| `web` | httptest 覆盖 API、状态码、CSRF、Host、Content-Type、请求体限制、安全头、SPA 回退、会话过期 |

- 运行 `go test ./...` 与 `go test -race ./...`。
- 对 LIKE 转义函数增加 fuzz 测试，确保 `%`、`_`、`\` 不逃逸。
- 不依赖真实网络和外部数据库。

### 12.2 前端测试

- Vitest + `@testing-library/svelte`。
- 覆盖：
  - 路由匹配与参数解析；
  - API 客户端错误映射、CSRF 重试、401 处理；
  - 解锁表单错误展示与密码清空；
  - 关键列表/分页渲染。
- `svelte-check --tsconfig ./tsconfig.json` 在构建与 CI 中必须通过。

### 12.3 手工测试

- Chrome、Edge、Firefox 最新两个大版本；Windows 10/11。
- 360px 与 1280px 布局。
- 开发模式（Vite 5173 + Go 8080）与生产二进制。
- 深链接刷新、前进/后退、404。
- 解锁后刷新、空闲超时、手动锁定、服务重启。
- 空数据目录首次启动自动建库；损坏数据库文件拒绝启动且不被覆盖。
- 修改 `config/note.yaml` 后配置生效，命令行参数可覆盖。
- 关闭浏览器重开、备份恢复。
- 检查响应头、Cookie、缓存策略、浏览器存储。

### 12.4 安全与性能测试

- 加密后对主库与 WAL 执行标记文本检索，必须无匹配。
- 错误密码、篡改密文、篡改 KDF 参数均返回统一错误。
- 跨站写请求、非法 Host、非 JSON 写请求被拒绝。
- 日志检查不含敏感字段。
- 构造 10,000 条笔记、约 100 MB 数据，验证列表 ≤100 ms、搜索 ≤300 ms。
- 测量首屏 JS/CSS gzip 体积 ≤150 KB。
- 测量解锁 KDF 耗时（预期 100–500 ms）与峰值内存。

### 12.5 完成标准

- PRD 第 9 节全部验收项通过。
- Go 测试、前端测试、`svelte-check` 全部通过。
- 干净环境 `just build` 成功，单二进制可在无 Node 环境运行。
- 明文残留检查通过。
- 安全自检清单（PRD 附录 B）逐项确认。

---

## 13. 性能与容量

| 指标 | 目标 |
| --- | --- |
| 冷启动 | ≤ 2 秒 |
| 列表 API | 10k 笔记 / 100 MB 下 ≤ 100 ms |
| 搜索 API | 同等数据下 ≤ 300 ms |
| 解密 1 MiB | ≤ 50 ms |
| KDF | 100–500 ms，属于安全成本 |
| 常驻内存 | 通常 < 100 MB；KDF 峰值 +64 MB |
| 首屏前端资源 | ≤ 150 KB gzip |
| 单条正文 | ≤ 1 MiB |

策略：

- 列表固定 20 条，避免一次返回全量。
- 搜索使用 `LIKE` 全表扫描；在目标数据量内可接受，后续可引入 FTS5 trigram。
- 单连接数据库串行处理；个人场景足够，避免并发复杂度。
- 不引入缓存层；API 全部 `no-store`，避免明文/密文缓存歧义。
- KDF 单并发，防止内存峰值叠加。

---

## 14. 部署与运维

### 14.1 运行

```bash
./note                                              # 读取 config/note.yaml
./note --addr 127.0.0.1:9090 --db ./data/notes.db   # 命令行覆盖
```

- 首次运行自动创建数据目录、数据库文件并执行初始化迁移（见 5.2）。
- 默认只允许本机访问。
- 远程绑定必须显式 `--allow-remote`，且应置于 TLS + 认证反向代理之后；MVP 不提供这些能力。

### 14.2 备份

1. 停止应用，或使用 SQLite 在线备份命令。
2. 复制 `notes.db`、`notes.db-wal`、`notes.db-shm`。
3. 加密笔记仍为密文；明文笔记仍为明文，备份需同等保护。
4. 旧备份可能包含加密前的明文，应用无法追溯清理。

### 14.3 升级与回滚

- 升级：停止应用 → 备份数据库 → 替换二进制 → 启动；启动时执行迁移。
- 迁移只向前；不支持自动回滚 schema。
- 回滚需恢复旧二进制与升级前备份。
- 发布前在数据库副本上演练迁移。

### 14.4 日志与监控

- 日志输出 stdout，由用户自行重定向或交给系统服务管理。
- 不提供远程遥测、指标上报或云服务。
- 进程存活以端口可访问为准；MVP 不增加健康检查端点。

---

## 15. 实施计划

### 15.1 M1：基础能力

- Go 模块、`config/note.yaml` 加载与校验、日志、HTTP 服务骨架。
- SQLite 启动检测与自动建库、PRAGMA、`sql/` 迁移、CRUD、搜索。
- API：列表、创建、详情、更新、删除、CSRF。
- Svelte 脚手架、路由、列表/详情/编辑视图、API 客户端。
- Vite → Go embed 构建链路、justfile。
- 对应验收：AC-01 ~ AC-06、AC-17、AC-25、AC-26。

### 15.2 M2：加密能力

- `crypto` 包、KDF、AES-GCM、参数校验。
- 加密创建、现有笔记加密、解锁、锁定、重加密。
- 解锁会话、CSRF 完整校验。
- 密码提示、忘记密码、删除。
- Svelte 加密/解锁/忘记密码视图。
- 对应验收：AC-07 ~ AC-16、AC-23、AC-24。

### 15.3 M3：加固与发布

- Host、安全头、CSP、日志脱敏。
- `secure_delete`、WAL checkpoint、明文残留验证。
- 完整单元/集成/前端测试。
- README、运维说明、备份演练。
- 浏览器手工验证、性能测试、单二进制发布。
- 对应验收：全部 AC。

### 15.4 文件级任务

```text
[ ] go.mod / cmd/note/main.go
[ ] config/note.yaml 与 internal/config（加载、合并、校验）
[ ] sql/（0001_init.sql、embed.go）
[ ] internal/store（启动检测、PRAGMA、迁移、CRUD、搜索）
[ ] internal/crypto（参数、容器、KDF、AEAD、测试）
[ ] internal/session
[ ] internal/web（中间件、API、静态资源、SPA 回退）
[ ] web 脚手架（Vite/Svelte/TS/router/api client）
[ ] web 视图与组件
[ ] justfile
[ ] 测试与安全检查
[ ] README / 运维说明
```

---

## 16. 决策记录（ADR）

以下为当前默认决策；涉及 PRD 开放问题的条目，以 Q1/Q2/Q3/Q5 的最终确认为准。

| 编号 | 决策 | 理由 | 代价 |
| --- | --- | --- | --- |
| ADR-01 | 前端使用 Svelte 5 + Vite，不使用 SvelteKit | 轻量、无 SSR/文件路由负担，构建产物直接嵌入 Go | 需要自写路由 |
| ADR-02 | 自写约 60 行 History 路由，不引入路由库 | 仅 6 个视图，减少依赖与体积 | 路由功能有限，复杂需求需扩展 |
| ADR-03 | JSON API + SPA 回退 | 前后端解耦，便于独立前端构建 | 需要维护 API 契约 |
| ADR-04 | SQLite 单连接 | 避免并发写与 PRAGMA 状态问题，个人场景足够 | 查询串行化 |
| ADR-05 | 不保存密码哈希 | 减少离线爆破目标；GCM tag 已验证密码 | 每次校验都需完整 KDF + 解密 |
| ADR-06 | 每条笔记独立密码与提示 | 灵活，单条笔记泄露不影响其他笔记 | 用户需管理多个密码 |
| ADR-07 | 标题、提示、时间为明文 | 支持列表、提示展示与搜索 | 元数据泄露 |
| ADR-08 | `dist` 不提交，构建时生成 | 避免产物与源码漂移，单二进制发布 | 直接 `go build` 前必须先构建前端 |
| ADR-09 | 不提供修改密码/取消加密 | 符合不可逆加密语义 | 需新建笔记迁移内容 |
| ADR-10 | 错误密码与损坏统一报错 | 避免泄露解密细节 | 用户无法区分密码错误与数据损坏 |

---

## 17. 待确认问题

| 编号 | 问题 | 当前默认 |
| --- | --- | --- |
| Q1 | 「单向加密」是否确认按 PRD 2.3 解释 | 密码正确可查看；无找回/后门 |
| Q2 | 每条笔记独立密码还是全局主密码 | 每条笔记独立 |
| Q3 | 是否接受标题、时间、提示明文 | 接受，界面明确告知 |
| Q4 | 是否需要解锁后搜索加密正文 | MVP 不做 |
| Q5 | 是否需要修改密码或导出加密备份 | MVP 不做 |
| Q6 | 是否需要应用启动口令 | MVP 不做，仅回环监听 |

以上问题与 PRD 第 13 节保持一致；开发前应确认 Q1，避免产品语义偏差。

---

*文档结束。*
