# note

本地单用户纯文本笔记应用：Go + SQLite + Svelte 5，单二进制部署，支持本地模糊搜索和带密码提示的单向加密笔记（无密码找回）。

> **项目状态：可运行骨架。** 已完成产品需求、技术方案，以及前后端脚手架、配置加载、SQLite 自动建库迁移和内嵌前端；笔记业务尚未实现。构建细节见 [构建指南](docs/BUILD.md)。

## 特性

- 纯文本笔记的创建、查看、编辑、删除。
- 列表按更新时间倒序、分页展示。
- 标题与明文正文的子串搜索；加密笔记仅标题可搜索。
- 加密笔记使用 Argon2id + AES-256-GCM，正文在数据库中只保存密文。
- 加密后不可取消；忘记密码后正文永久无法恢复，不提供修改密码、密码找回或密钥托管。
- 密码提示问答（最多 3 组），仅辅助记忆，明文保存，不能用于解密。
- 解锁会话空闲 10 分钟自动失效，支持手动锁定；服务重启后全部失效。
- Svelte 5 轻量前端，构建产物通过 `go:embed` 嵌入 Go 二进制。
- 默认只监听 `127.0.0.1`，不依赖云服务、账号或外部网络资源。

## 安全边界

请在使用前理解以下限制：

- **忘记密码 = 正文永久无法恢复。** 没有找回、重置、后门或恢复密钥。
- 未加密笔记的正文以明文保存在 SQLite 中；加密笔记的正文才以密文保存。
- 标题、创建/更新时间、密码提示问答是明文元数据，不是密文。
- 加密笔记正文不参与搜索，只能通过标题匹配。
- 应用不防御已经控制本机的攻击者，例如键盘记录、内存抓取和截屏。
- 默认仅允许本机访问；不要在没有 TLS 和认证的情况下将端口暴露到公网。
- 应用只保证运行时数据库与 WAL 的明文清理，不保证 SSD 物理层、文件系统快照或旧备份中的历史数据被擦除。

## 技术栈

| 层        | 技术                                                     |
| --------- | -------------------------------------------------------- |
| 后端      | Go 1.26+，`net/http` + `ServeMux`                        |
| 数据库    | SQLite（`modernc.org/sqlite`，纯 Go，无 CGO）            |
| 加密      | Argon2id（64 MiB）+ AES-256-GCM                          |
| 前端      | Vite + Svelte 5 + TypeScript，不使用 SvelteKit           |
| 路由/样式 | 自写 History 路由；Svelte scoped CSS                     |
| 构建      | Vite 输出到 `internal/web/dist`，`go:embed` 嵌入单二进制 |
| 运行环境  | Node/npm 仅用于开发与构建；运行机只需要 Go 二进制        |

## 架构

```text
Svelte 5 SPA
   │  fetch /api + X-CSRF-Token
   ▼
Go net/http
   ├── SQLite (data/notes.db, WAL)
   ├── Argon2id + AES-256-GCM
   ├── 内存解锁会话
   └── //go:embed internal/web/dist
```

前端和 API 同源：开发时 Vite 把 `/api` 代理到 Go，生产时由 Go 同时提供页面、静态资源和 API。

## 文档

- [产品需求文档（PRD）](docs/PRD.md)
- [技术方案](docs/TECH_DESIGN.md)
- [构建指南](docs/BUILD.md)
- [变更记录](CHANGELOG.md)

## 快速开始

前置条件：

- Go 1.26 及以上
- Node.js 22 LTS 及以上（建议 24 LTS）
- npm
- `just`（可选，推荐）

首次构建（Go 的 `//go:embed` 需要 `internal/web/dist`）：

```bash
just install   # npm ci
just build     # 构建前端 + 单二进制
./note         # Linux/macOS
./note.exe     # Windows
```

不使用 `just` 时：

```bash
npm --prefix web ci
npm --prefix web run build
go run ./cmd/note
```

打开 <http://127.0.0.1:8080>。首次启动会自动创建 `data/notes.db`。

### 前端开发

Go 需要已存在一次构建好的 `internal/web/dist` 才能编译。之后开两个终端：

```bash
# 终端 1：后端
go run ./cmd/note

# 终端 2：Vite 开发服务器，/api 代理到 127.0.0.1:8080
cd web
npm run dev
```

### 构建单二进制

```bash
just build
./note     # Linux/macOS
./note.exe # Windows
```

不使用 `just` 时：

```bash
npm --prefix web ci
npm --prefix web run build

# Linux/macOS
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o note ./cmd/note
```

Windows PowerShell（输出必须是 `note.exe`）：

```powershell
npm --prefix web ci
npm --prefix web run build
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o note.exe ./cmd/note
```

部署时只需要 `note`（Windows 为 `note.exe`）和 `config/`、`data/` 目录，不需要 Node/npm，也不需要单独部署前端。

### 测试与检查

```bash
go test ./...
go test -race ./...

npm --prefix web test
npm --prefix web run check
```

或直接执行 `just test`、`just check`。

## 配置

配置优先级：内置默认值 < `config/note.yaml` < 命令行参数。

| 参数             | 默认值               | 说明                                   |
| ---------------- | -------------------- | -------------------------------------- |
| `--config`       | `./config/note.yaml` | YAML 配置文件路径                      |
| `--addr`         | `127.0.0.1:8080`     | 监听地址                               |
| `--db`           | `./data/notes.db`    | SQLite 文件路径                        |
| `--allow-remote` | `false`              | 允许绑定非回环地址；启动时打印安全警告 |

```bash
go run ./cmd/note --addr 127.0.0.1:9000 --db ./data/notes.db
```

## 数据与备份

- 数据默认保存在 `data/notes.db`，同时可能出现 `notes.db-wal`、`notes.db-shm`。
- 备份前建议停止应用，或使用 SQLite 在线备份：
  ```bash
  sqlite3 data/notes.db ".backup 'data/backup.db'"
  ```
- 加密笔记在备份中仍是密文；明文笔记在备份中仍是明文，需要同等保护。
- 旧备份可能包含加密前的历史明文，且无法通过应用清理。

## 项目结构

```text
note/
├── cmd/note/              # Go 入口
├── config/                # note.yaml 默认配置
├── sql/                   # 建表与迁移脚本（嵌入二进制）
├── internal/
│   ├── config/            # 运行参数
│   ├── store/             # SQLite、迁移、查询
│   ├── crypto/            # Argon2id + AES-GCM（待实现）
│   ├── session/           # 内存解锁会话（待实现）
│   └── web/               # API、中间件、内嵌前端
│       └── dist/          # Vite 构建产物（不提交）
├── web/                   # 独立 Svelte 5 前端工程
├── data/                  # 运行时数据（不提交）
├── docs/                  # PRD、技术方案、构建指南
├── CHANGELOG.md
└── justfile
```

## 实现路线

- [x] 产品需求文档
- [x] 技术方案
- [x] Go 后端骨架与 SQLite 自动建库迁移
- [x] Svelte 前端骨架与构建嵌入
- [ ] 明文笔记 CRUD、分页、模糊搜索
- [ ] 加密、解锁、提示问答与会话
- [ ] 安全加固、测试与单二进制发布

## 许可证

[Apache License 2.0](LICENSE)
