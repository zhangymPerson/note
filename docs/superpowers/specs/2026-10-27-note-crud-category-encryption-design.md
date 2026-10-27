# 设计：笔记 CRUD + 分类管理 + 全局主密码加密（浏览器端加解密）

日期：2026-10-27 · 状态：待评审 · 替代 PRD v1.2 的 5.3/8.4/8.5 与开放问题 Q2 默认值

## 1. 需求与对 PRD 的偏离

| 项 | 本设计 | PRD v1.2 原案 | 偏离原因 |
| --- | --- | --- | --- |
| 密码模型 | 全局主密码，只配置一次；忘记则所有加密笔记永久不可读 | 每条笔记独立密码（Q2 默认） | 用户明确要求 |
| 解密位置 | 浏览器端 WebCrypto，服务端只存密文字节 | Go 服务端解密 + 内存会话 | 用户要求简化；本地单机场景增益等价 |
| 解锁会话 | 密钥仅存页面内存，刷新/锁定即失效 | 服务端 10 分钟空闲会话 | 无服务端状态，天然满足"重启失效" |
| 助记提示 | 单条问题文本，全局一份，如「你的生日是多少？」 | 每笔记最多 3 组问答 | 用户要求；不存答案，防泄露密码构成材料 |
| 正文长度 | ≤ 2000 个 Unicode 字符 | ≤ 1 MiB | 用户要求 |
| 分类 | 新增：笔记可选分类，分类可管理 | 非目标（2.2 标签/文件夹） | 用户要求，覆盖非目标 |

不变的安全底线（继续有效）：密码与派生密钥绝不落盘、不进日志；密文列 CHECK 约束；CSRF/Host/安全头中间件；`no-store`；禁止 `{@html}`；密码/密钥不写 localStorage。

## 2. 加密格式（跨客户端规范，CLI 依此实现）

```
主密码设置（浏览器）：
  master_salt = random(16)                       → master_password.salt
  question    = 用户输入助记问题（≤100 字符）      → master_password.question
  密码本身永不发往服务端。

密钥派生（任何客户端）：
  key = PBKDF2-HMAC-SHA256(NFC(password), master_salt, iter, 32B)
  iter 取自 notes.kdf_params JSON，当前固定 150000。

加密（浏览器，对正文 UTF-8 字节）：
  nonce  = random(12)
  aad    = "note:v1:" + note_id
  cipher = nonce || AES-256-GCM(key, nonce, plaintext, aad)   // 含 16B tag

解密（任何客户端）：
  按上式反向 Open；tag 校验失败 → 统一「密码错误或数据损坏」，
  不区分密码错/篡改/参数非法。
```

- 参数自描述：`kdf_params = {"algo":"pbkdf2","prf":"HMAC-SHA256","iter":150000,"keylen":32}`；读取时校验 `iter ∈ [100000, 1000000]`、`keylen = 32`，超界视为数据损坏拒绝解密。
- AAD 绑定笔记 ID：跨笔记移植密文无法解密。
- 版本前缀 `note:v1:` 预留未来格式升级。
- WebCrypto 与 Node 22+ / 各语言标准库均内置 PBKDF2-SHA256 与 AES-GCM，CLI 零障碍。
- 诚实边界：PBKDF2 快于 Argon2，弱密码离线爆破成本低于 PRD 原案；界面文案要求使用长口令（≥12 字符），风险在 PRD R4 可接受范围内重新声明。

## 3. 数据模型（迁移 `sql/0004_note_app.sql`）

```sql
CREATE TABLE categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at TEXT NOT NULL                      -- UTC 秒级文本约定
);
-- 默认分类固定 id=1「未分类」，迁移内插入，不可删除、不可改名（应用层守卫）。

CREATE TABLE master_password (
    id         INTEGER PRIMARY KEY CHECK (id = 1),  -- 单行表
    salt       BLOB NOT NULL,                       -- 16B
    question   TEXT NOT NULL,
    created_at TEXT NOT NULL
);

ALTER TABLE notes ADD COLUMN category_id INTEGER REFERENCES categories(id);
-- NULL = 未分类；加密笔记照常可选分类（元数据明文）。
-- notes.hints 列保留但不再写入（历史无数据）；kdf_salt 列在加密时冗余写入
-- master_salt 的副本（满足 0001 的 CHECK：加密态 kdf_salt NOT NULL），
-- 客户端以 master_password.salt 为准，两者不一致时视为数据损坏。
```

- 笔记加密态复用现有列：`is_encrypted=1` 时 `content IS NULL`、`cipher/kdf_params NOT NULL`（0001 的 CHECK 原样适用）。
- 分类删除：`DELETE FROM categories WHERE id=?` 前检查 `notes` 中引用数，非零返回 409（响应含笔记数）。

## 4. API（全部经现有 hostGuard/CSRF/securityHeaders 链）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/notes?q=&page=&category=` | 列表+搜索，updated_at 倒序，每页 20；加密笔记仅标题命中；命中 `q` 记录活动（现有 SearchNotes 扩展） |
| POST | `/api/notes` | `{title, content? \| cipher_b64?, kdf_params?, category_id?, encrypted}`；201 返回笔记元数据 |
| GET | `/api/notes/{id}` | 明文笔记含 content；加密笔记只返回元数据+`encrypted:true`（密文 b64 一并返回供前端解密） |
| PUT | `/api/notes/{id}` | 同 POST 字段；rev+1；加密笔记只接受新密文 |
| DELETE | `/api/notes/{id}` | 204；加密笔记删除不需要密码 |
| GET | `/api/master-password` | `{configured, question}`（未配置时 question 为空） |
| POST | `/api/master-password` | `{salt_b64, question}`；仅一次，已存在返回 409 |
| GET | `/api/categories` | 列表 + 每类笔记计数 |
| POST / PUT / DELETE | `/api/categories[/{id}]` | 新增/改名/删除；id=1 拒改拒删 409；有笔记拒删 409 |

- 校验（400 + 字段级错误）：标题 1–200 字符；正文 ≤ 2000 字符（`utf8.RuneCountInString`）；分类名 1–30 字符非重复；密文 b64 ≤ 64 KiB；请求体上限沿用 decodeJSON。
- 服务端永不明文参与：不校验密文内容合法性以外的任何密码字段；加密笔记的 cipher 对服务端是黑盒字节。

## 5. 前端（Svelte 5，无新依赖）

- `lib/crypto.ts`：`deriveKey(password, salt, iter)`、`encryptNote/decryptNote`（WebCrypto，密钥仅内存变量）；`lib/session-key.ts` 持有当前会话密钥 + `lock()`。
- 视图：列表（分类筛选条 + 搜索 + 分页 + 「已加密」标记）、详情（明文直显；加密未解锁显示密码框 + 助记问题折叠区 + 忘记密码说明）、编辑器（分类下拉、加密勾选、2000 字符计数、加密时若未配置主密码先跳设置页）、`/categories` 分类管理（增改删 + 计数 + 删除确认）、主密码设置页（密码/确认/助记问题 + 不可恢复警告勾选）。
- 解锁后密钥驻留内存：同页浏览其他加密笔记不再询问；刷新或点顶栏「锁定」需重输。
- 忘记密码页：展示问题文本，声明无找回，仅「返回」或「删除笔记」。

## 6. 测试

- Go：store（分类 CRUD/409/默认分类守卫、2000 字符边界、密文存取、迁移版本 4）、web httptest（全端点状态码、CSRF、加密笔记不返回 content、master-password 一次性）。
- 前端 Vitest：crypto.ts 用 Node 内置 WebCrypto 做派生+加解密往返、错误密码拒绝、AAD 跨笔记移植失败、iter 边界；分类/列表 API 客户端映射。
- 手工：Playwright 走「设主密码→加密→刷新→解锁→编辑→锁定→删分类被拒→CLI 解密同一笔记」全链路；`just test` + `just check` 为合入门槛。

## 7. 文档同步

实现完成后：PRD 升 v1.3（上表偏离项 + 验收项增改：删 AC-10/16 会话类，增分类与 2k 验收），TECH_DESIGN 的 8.4/8.5 替换为本设计第 2、5 节内容。
