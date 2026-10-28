# 分类搬进「全部笔记」页右侧树（设计）

日期：2026-10-28
状态：待确认

## 意图与为什么

现在分类有两个入口：顶部导航的「分类」tab（`/categories`，一个独立的增删改查页面），
以及列表页顶部一排 chips（只做筛选）。同一个概念分散在两地，改个分类名要去另一页，
看完还得点回列表。改成：**导航去掉「分类」，`/categories` 页面删除，分类以树的形式
常驻在「全部笔记」页右侧，筛选和增删改都在这一页完成。**

## 目标

- 顶部导航不再有「分类」；`/categories` 不再渲染独立页面。
- 「全部笔记」页变成左右两栏：左侧列表（搜索 + 条目 + 分页），右侧分类树。
- 右侧树：根节点「全部笔记」（总数、可展开收起）+ 子项「未分类」+ 各分类，每项显示名称与笔记数。
- 树内就地完成：按分类筛选、新建分类、改名、删除，含错误提示。
- 现有行为不退化：URL 里的 `category`/`q`/`page` 参数、分页、删除笔记后计数刷新、
  默认分类不可改删、删除非空分类的 409 文案。

## 非目标

- 不做多级分类（`categories` 表没有 `parent_id`，需要迁移 + 后端改造，本次只改前端）。
- 不把分类树挂到详情页/编辑页（`Layout.svelte` 不动）。
- 不改任何后端接口、数据库结构、加密逻辑。
- 不改编辑器里已有的分类下拉（`NoteEditorView.svelte` 保持原样）。

## 约束

- 只新增/修改 `web/src` 下的文件与 `docs`；后端零改动。
- 分类名是用户输入，只能用文本插值渲染，不得进 `{@html}`。
- 状态变更走既有 `lib/notes.ts` 客户端（`X-CSRF-Token` 已由 `lib/api.ts` 统一处理）。
- 「未分类」= `id 1`，不可改名/删除，界面不显示操作按钮（沿用 PRD FR-4.5）。

## 做法（已选 A）

| 方案 | 内容 | 取舍 |
|------|------|------|
| **A（选它）** | 新增 `components/CategoryTree.svelte` 只管画和收事件；`categories` 状态与全部 API 调用留在 `NoteListView.svelte` | 改动集中，不引入新状态层；树只在列表页存在——正是需求 |
| B | 分类数据抽成共享 store `lib/categories.ts`，列表页/树/编辑器共用 | 多一层缓存失效要维护，只有一个改动点时收益不抵复杂度 |
| C | 树挂到 `Layout.svelte`，全站常驻 | 超出需求范围，编辑页会同时出现分类下拉与分类树，语义重复 |

## 组件契约

`web/src/components/CategoryTree.svelte`

```ts
interface Props {
  categories: Category[];          // 来自 listCategories()
  active: number;                  // 0 = 全部；否则分类 id
  busy: boolean;                   // 任一写操作进行中，禁用所有操作
  onPick: (id: number) => void;    // 0 = 全部
  onCreate: (name: string) => Promise<string>;   // 返回错误文案，'' = 成功
  onRename: (id: number, name: string) => Promise<string>;
  onDelete: (id: number) => Promise<string>;
}
```

回调返回错误字符串（而不是抛异常）：每行的错误就地显示在对应节点下方，
跟现在的分类管理页行为一致（409 `category_not_empty` 的文案直接照搬）。

组件内部状态只有：根节点展开/收起、正在改名的节点 id 与输入值、新建输入框的值、
各节点的错误文案。**分类数据本身不落地在组件里**。

## 交互

- 根节点「全部笔记」：点击=筛选全部（`category=0`），左侧三角切换展开/收起，显示总数
  （`sum(categories.note_count)`，与现在 chips 的「全部」算法一致）。默认展开。
- 子项行：点击=按该分类筛选（并 `page = 1`）。行尾 hover/focus 出现「改名」「删除」两个
  小按钮；「未分类」行不渲染它们。
- 改名：行内变输入框，Enter 保存、Esc 取消、失焦不保存（避免误提交）。
- 新建：树底部常驻输入框 +「+」，Enter 提交；成功后清空输入，失败把文案显示在输入框下。
- 删除：`window.confirm`（沿用现页面做法）；若删的是当前筛选的分类，`category` 回 0 并回到全部。
- 视觉：缩进 + 左侧竖线表示层级；选中项高亮（沿用 chip 的 `data-active` 思路，改成行样式）。
  不做 ARIA `role="tree"`（那套要求方向键导航与 roving tabindex，成本高且易做错）；
  用 `<nav aria-label="分类">` + `<ul>` + `<button>`，键盘 Tab 可达即可。

## 要改的文件

1. **新增** `web/src/components/CategoryTree.svelte` —— 上述树组件。
2. **改** `web/src/views/NoteListView.svelte`
   - 删掉 chips 区块与 `pickCategory` 的 chips 版本，改为渲染 `<CategoryTree>`。
   - 加 `createCategory` / `renameCategory` / `deleteCategory` 三个包装函数，返回错误文案；
     成功后更新 `categories`（rename 后端直接返回全量列表；create/delete 后重拉一次）。
   - 删除当前筛选分类时 `category = 0`、`page = 1`。
   - 结构改为 `.list-layout`（grid：`minmax(0,1fr) 240px`），窄屏（`max-width: 720px`）
     单列堆叠，树排在列表上方。
   - `syncUrl()` 不变（继续把 `q`/`category`/`page` 写进 `/notes`）。
3. **删除** `web/src/views/CategoryManagerView.svelte`。
4. **改** `web/src/router/router.ts`
   - `RouteName` 去掉 `"categories"`；`patterns` 里 `/categories` 映射到 `list`
     （旧书签不 404；`NoteListView` 挂载时 `syncUrl()` 会 `replaceState` 把地址纠正成 `/notes`）。
5. **改** `web/src/App.svelte` —— 删 `categories` 的 `titles` 键与视图分支。
6. **改** `web/src/components/Layout.svelte` —— 删导航里的「分类」。
7. **改** `web/src/app.css` —— 新增 `.list-layout` / `.cat-tree` / 行与输入框样式；
   删除只有 chips 用的 `.chips` `.chip` `.chip-num`，和只有分类管理页用的 `.inline-form` `.manager*`。
   （`.detail` `.card` 仍被其它页面使用，保留。）
8. **改** `web/src/router/router.test.ts` —— `/categories` 的断言改为映射到 `list`。
9. **改** `docs/PRD.md`、`docs/TECH_DESIGN.md` —— 路由表与 5.5 节里 `/categories` 的表述、
   页面清单、`views/` 目录说明同步成「分类树在列表页右侧」。
10. **改** `CHANGELOG.md` —— 按 AGENTS.md，`just test` 与 `just check` 通过后写 `[Unreleased]` 条目。

## 留给实现者的判断

- 树节点 hover 按钮的具体图标/文案（「改名」「删除」两个小按钮即可，不需要图标库）。
- 窄屏下树是否需要默认收起（默认展开、可手动收起即可）。
- 新增测试：现有前端测试只覆盖 `lib/` 与路由，不引入组件测试框架；本次只更新路由断言。
