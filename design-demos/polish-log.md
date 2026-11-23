# 三方向原型 · 打磨日志（2026-11-23）

本环境无法目视图（图片 base64 会被截断），打磨环改用程序化验证 + 读代码。

## 验证脚本

- `shoot-demos.js`（scratch）：截图（每方向 首页×3主题 / 列表×3主题 / 详情×3主题 / 编辑器 / 编辑器+AI / 锁定卡 / 移动首页 / 移动列表 = 14 张）+ 断言（JS 错误、字体栈解析、横向溢出、顶栏、列表行数、搜索筛选、详情正文、主题循环、正文对比度）。
- `probe-detail2.js`：每主题×列表页的语义色对比度矩阵（muted/faint/danger/品牌/导航/分类徽标）。
- `probe-states.js`：热力图 84 格 + 4 色阶、空态卡 + CTA、合并模式勾选框 + 合并栏、移动端 375 无横向溢出。
- `probe-overflow.js`：溢出元素定位（哪个元素 right > viewport）。

## 发现并修复的问题

### 1. faint（时间戳/计数/提示小字）亮色与护眼对比度不足（2.7–2.9:1）

- 亮 `--faint: #9a9489 → #837d6f`（3.73:1）
- 护眼 `--faint: #83907a → #75816a`（3.50:1）
- 暗色原值达标，未动。三份文件同步改。

### 2. 列表行底部操作按钮用 faint 色，在 surface 上对比不足（3.2–3.7:1）

- `.note-foot button` 颜色 faint → muted。三份文件同步改。

### 3. 暗色分类徽标字（11px）对比度 2.7:1 不足

- `.badge.dim` 暗色下 `color: var(--faint)` → `var(--muted)`（5.46:1）。三份文件同步改。

### 4. 移动端列表页横向溢出（sw=476/444 vs 375）

- **A/C**：`.list-bar` 内搜索框（input 最小宽 ~177px）+ 计数 + 合并 + 新建 挤爆 375px。
- **B**：额外有 `.note-main` 列模式下 `align-items:flex-start` + 摘要 `white-space:nowrap` → 摘要按内容宽撑到 831px。
- 修复：三份在 `html[data-device="mobile"]` 与 `@media (max-width:640px)` 同时加
  `.list-bar{flex-wrap:wrap}` + `.list-bar .search-form{flex:1 1 100%}` + `.search-input{min-width:0}`；
  B 把移动端 `.note-main` 改 `align-items:stretch` 并给 `.note-summary` 加 `white-space:normal`（移动端摘要允许折行，不再吃 nowrap 的固有宽度）。

### 5. 探针时序误报（非页面 bug）

- 列表页每次 render 先出 420ms 骨架屏；搜索/合并断言等 400ms 不够 → 统一等 900ms。
- 合并模式最初报「无勾选框」即此因，修正探针后 `hasCheckboxes: 7 / barVisible: true` 通过。

### 6. 字体断言误报（非页面 bug）

- Google Fonts 按 unicode-range 分片只加载实际使用的字重；`check('600 …')` 对未使用字重返回 false。
- 改为断言：computed family 优先解析到 `"Noto Serif SC"` + 至少一个分片 loaded（探针确认 400/700 已 loaded、零请求失败、h1 栈正确）。

## 最终状态

- 三方向 × 三主题 × 首页/列表/详情 对比度矩阵全部达标（≥4.5 正文 / ≥3.0 小字）。
- 热力图 84 格 4 色阶、空态卡、合并模式、移动端 375 无溢出、零 JS/控制台错误。
- 完整断言全部通过。
