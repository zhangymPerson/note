# 新建笔记模板（note-example.md）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新建笔记时正文预填一份可由用户修改的 markdown 骨架，骨架存放在 `note.yaml` 同目录的 `note-example.md`，页面上有入口可以改它。

**Architecture:** Go 侧新增小包 `internal/template`（纯函数定位 + `go:embed` 默认值 + 原子写），`note web` 启动时缺文件就补一份；`internal/web` 暴露 `GET/PUT /api/template`。前端新增 `lib/template.ts` 客户端与 `/template` 独立编辑页（textarea + markdown 预览），新建笔记页拉模板预填正文，并借既有 `editorDraft` 交棒机制跳去编辑模板。

**Tech Stack:** Go 标准库（`net/http`、`go:embed`、`os`）+ Svelte 5 + TypeScript + Vitest。

**Spec:** `docs/superpowers/specs/2026-10-29-new-note-template-design.md`

## Global Constraints

- 模板长度上限与正文一致：**2000 码点**（后端 `internal/web/handlers_notes.go` 的 `maxContentRunes`，前端 `MAX_CONTENT_CODEPOINTS`），超限返回 400 `validation_failed` + `fields.content`。
- 模板文件固定为 `filepath.Join(filepath.Dir(ConfigPath), "note-example.md")`；路径只能由 `internal/template.Path` 推导，不在别处拼。
- 初始化**绝不覆盖**已存在的模板文件；配置目录不可写只 `slog.Warn`，不影响启动。
- 写文件必须原子：同目录临时文件 + `os.Rename`；文件权限 `0644`，目录 `0700`。
- `PUT /api/template` 是状态变更接口，必须走既有 `csrfProtect` 双提交校验，**不得**扩大 `/api/shutdown` 的豁免范围。
- `{@html}` 仍只允许出现在 `web/src/components/Markdown.svelte`；模板内容只以文本插值或 `Markdown.svelte` 呈现。
- 响应一律 `Cache-Control: no-store`（`writeJSON` 已统一处理），响应体不含正文/密文/盐/令牌；`path` 字段属于与 `/api/info` 的 `db_path` 同尺度的路径信息，允许返回。
- 中文文案直接照抄本计划中的字符串，不要自行改写。
- 时间戳、日志字段、数据库结构本次**零改动**（不加迁移）。

## Review Focus

- 配置目录不存在或只读：`note web` 仍必须正常启动，模板 GET 回退内置默认值（`source: "default"`），新建笔记正文为空而不是页面报错。
- 用户手工删掉 `note-example.md` 后重启：文件被重新创建，GET 不 500。
- 模板写入超长内容（2000+ 码点，含 emoji）：PUT 拒绝并给出字段级文案，新建笔记不会因模板而一保存就超限。
- 模板被清空成空文件：新建笔记正文为空，编辑器与预览区表现与改动前一致。
- 在新建笔记页点「编辑模板」再返回：已填的标题、分类、正文不丢（草稿交棒）。
- 模板里写 `<script>` 或 `javascript:` 链接：`/template` 预览走既有 DOMPurify 管道，不执行、不注入。

---

### Task 1: `internal/template` 包

**Files:**

- Create: `internal/template/template.go`
- Create: `internal/template/default.md`
- Test: `internal/template/template_test.go`

**Interfaces:**

- Consumes: 无（纯新包）
- Produces:
  - `const FileName = "note-example.md"`
  - `func Path(configPath string) string`
  - `func Default() string`
  - `func Ensure(path string) (created bool, err error)`
  - `func Load(path string) (content string, fromDefault bool, err error)`
  - `func Save(path, content string) error`

- [ ] **Step 1: 写失败的测试**

`internal/template/template_test.go`：

````go
package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathSitsNextToConfigFile(t *testing.T) {
	got := Path(filepath.Join("/home", "me", ".config", "note", "note.yaml"))
	want := filepath.Join("/home", "me", ".config", "note", "note-example.md")
	if got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestDefaultIsSimpleMarkdownNote(t *testing.T) {
	body := Default()
	if strings.TrimSpace(body) == "" {
		t.Fatal("内置默认模板不能为空")
	}
	for _, marker := range []string{"# ", "- ", "```"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("默认模板缺少 markdown 标记 %q", marker)
		}
	}
}

func TestEnsureCreatesFileOnceAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)

	created, err := Ensure(path)
	if err != nil || !created {
		t.Fatalf("首次 Ensure() = %v, %v; want created=true, nil", created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != Default() {
		t.Fatalf("首次创建的内容应为内置默认值: %v", err)
	}

	if err := os.WriteFile(path, []byte("# 我改过的模板"), 0o644); err != nil {
		t.Fatalf("准备自定义模板: %v", err)
	}
	created, err = Ensure(path)
	if err != nil || created {
		t.Fatalf("二次 Ensure() = %v, %v; want created=false, nil", created, err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "# 我改过的模板" {
		t.Fatalf("Ensure 覆盖了用户模板: %q, %v", data, err)
	}
}

func TestLoadFallsBackToDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content, fromDefault, err := Load(path)
	if err != nil || !fromDefault || content != Default() {
		t.Fatalf("Load(缺失) = %q, %v, %v; want 内置默认值, true, nil", content, fromDefault, err)
	}
}

func TestLoadReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("# 自定义"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, fromDefault, err := Load(path)
	if err != nil || fromDefault || content != "# 自定义" {
		t.Fatalf("Load(存在) = %q, %v, %v", content, fromDefault, err)
	}
}

func TestSaveWritesAtomicallyAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	if err := Save(path, "# 第一版"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(path, "# 第二版"); err != nil {
		t.Fatalf("Save 覆盖: %v", err)
	}
	content, fromDefault, err := Load(path)
	if err != nil || fromDefault || content != "# 第二版" {
		t.Fatalf("Save 后内容 = %q, %v, %v", content, fromDefault, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("目录里只剩模板文件，发现临时文件残留: %v", names)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("模板文件权限 = %o, want 644", mode)
	}
}
````

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/template -v`
Expected: FAIL / build error —— `undefined: Path`、`undefined: Default` 等

- [ ] **Step 3: 写 `internal/template/default.md`**

内容固定为（结尾一个换行，不留多余空行）：

````markdown
# 标题

一句话写清这条笔记要记什么。

## 要点

- 第一条
- 第二条

## 待办

- [ ] 还没做的事

## 记录

> 引用的原文或想法放这里。

```go
// 需要留代码片段时用代码块
```
````

- [ ] **Step 4: 写 `internal/template/template.go`**

`Save` 的实现照 `internal/config/config.go` 的 `SaveUserName`：`os.MkdirAll(dir, 0o700)` → `os.CreateTemp(dir, ".note-example-*.md")` → 写入 → `Chmod(0o644)` → `Close` → `os.Rename`，`defer os.Remove(tmpName)` 兜住失败路径。`Load` 用 `os.ReadFile`，`errors.Is(err, fs.ErrNotExist)` 走默认值分支。

```go
// Package template 管理新建笔记的 markdown 骨架文件（note-example.md）。
// 骨架与 note.yaml 同目录，用户在页面或编辑器里改，服务端只管读写。
package template

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName 是骨架文件的固定文件名，与 note.yaml 放在同一目录。
const FileName = "note-example.md"

//go:embed default.md
var defaultContent string

func Path(configPath string) string

func Default() string

func Ensure(path string) (created bool, err error)

func Load(path string) (content string, fromDefault bool, err error)

func Save(path, content string) error
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/template -v`
Expected: PASS（6 个用例）

- [ ] **Step 6: 提交**

```bash
git add internal/template
git commit -m "feat(note): 新增新建笔记模板包，默认骨架内嵌且只在缺失时落盘"
```

---

### Task 2: `GET/PUT /api/template`

**Files:**

- Create: `internal/web/handlers_template.go`
- Modify: `internal/web/server.go`（在 `GET /api/settings` 一组路由后加两行）
- Modify: `internal/web/handlers_settings.go:96-116`（`application` 结构体与 `newApplication`）
- Test: `internal/web/template_api_test.go`

**Interfaces:**

- Consumes: `template.Path` / `template.Load` / `template.Save` / `template.Default`（Task 1）；`writeJSON` / `writeError` / `writeValidationError` / `decodeJSON`（`internal/web/errors.go`，已存在）；`maxContentRunes`（`internal/web/handlers_notes.go`，已存在）
- Produces:
  - `application.templatePath string` 字段（`newApplication` 里由 `template.Path(cfg.ConfigPath)` 赋值）
  - 响应体形状 `{"content":"…","path":"…","source":"file"|"default","default_content":"…"}`

- [ ] **Step 1: 写失败的测试**

`internal/web/template_api_test.go`：

```go
package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"note/internal/template"
)

type templateResp struct {
	Content        string `json:"content"`
	Path           string `json:"path"`
	Source         string `json:"source"`
	DefaultContent string `json:"default_content"`
}

func decodeTemplate(t *testing.T, body []byte) templateResp {
	t.Helper()
	var out templateResp
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode template response: %v", err)
	}
	return out
}

func TestTemplateAPIFallsBackToBuiltInDefault(t *testing.T) {
	handler, cfg := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodGet, "/api/template", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := decodeTemplate(t, rec.Body.Bytes())
	if body.Source != "default" || body.Content != template.Default() {
		t.Fatalf("文件缺失时应回退默认: %+v", body)
	}
	if body.Path != filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName) {
		t.Fatalf("path = %q", body.Path)
	}
	if body.DefaultContent != template.Default() {
		t.Fatal("default_content 应等于内置默认模板")
	}
}

func TestTemplateAPISavesAndReadsBack(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPut, "/api/template", token, `{"content":"# 我的骨架\n\n- 一\n- 二"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200: %s", rec.Code, rec.Body)
	}
	if body := decodeTemplate(t, rec.Body.Bytes()); body.Source != "file" || !strings.Contains(body.Content, "我的骨架") {
		t.Fatalf("PUT 响应 = %+v", body)
	}

	// 落盘位置与内容
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName))
	if err != nil || !strings.Contains(string(data), "我的骨架") {
		t.Fatalf("文件内容 = %q, %v", data, err)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/template", "", "")
	if body := decodeTemplate(t, rec.Body.Bytes()); body.Source != "file" || !strings.Contains(body.Content, "我的骨架") {
		t.Fatalf("GET 读回 = %+v", body)
	}
}

func TestTemplateAPIRejectsTooLongContent(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	tooLong := strings.Repeat("字", maxContentRunes+1)
	payload, err := json.Marshal(map[string]string{"content": tooLong})
	if err != nil {
		t.Fatal(err)
	}
	rec := apiRequest(t, handler, http.MethodPut, "/api/template", token, string(payload))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT 超长 = %d, want 400: %s", rec.Code, rec.Body)
	}
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "validation_failed" || body.Error.Fields["content"] == "" {
		t.Fatalf("错误体 = %+v", body.Error)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName)); !os.IsNotExist(err) {
		t.Fatal("校验失败时不该写文件")
	}
}

func TestTemplateAPIAllowsEmptyContent(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPut, "/api/template", token, `{"content":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 空内容 = %d, want 200: %s", rec.Code, rec.Body)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName))
	if err != nil || len(data) != 0 {
		t.Fatalf("空模板应写成 0 字节文件: %q, %v", data, err)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/template", "", "")
	if body := decodeTemplate(t, rec.Body.Bytes()); body.Source != "file" || body.Content != "" {
		t.Fatalf("空模板 GET = %+v", body)
	}
}

func TestTemplateAPIRequiresCSRFForWrites(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodPut, "/api/template", "", `{"content":"# x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无令牌 PUT = %d, want 403: %s", rec.Code, rec.Body)
	}
	if rec := apiRequest(t, handler, http.MethodGet, "/api/template", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET 不需要 CSRF，得到 %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web -run TestTemplateAPI -v`
Expected: FAIL —— 所有用例拿到 404 `not_found`（路由还不存在）

- [ ] **Step 3: 加 `templatePath` 字段**

`internal/web/handlers_settings.go`：`application` 结构体加 `templatePath string`，`newApplication` 里赋值 `templatePath: template.Path(cfg.ConfigPath)`，并 import `note/internal/template`。

- [ ] **Step 4: 写 `internal/web/handlers_template.go`**

```go
// handleGetTemplate 返回新建笔记的 markdown 骨架：文件读不到就回退内置默认值，
// 不让「模板文件被删/不可读」变成页面错误。
func (app *application) handleGetTemplate(w http.ResponseWriter, r *http.Request)

// handlePutTemplate 校验并原子写回骨架文件。
func (app *application) handlePutTemplate(w http.ResponseWriter, r *http.Request)
```

实现要点：

- 请求体 `struct{ Content string `json:"content"` }`，`decodeJSON(w, r, &body, 128<<10)`。
- GET：`content, fromDefault, err := template.Load(app.templatePath)`；`err != nil` 时 `slog.Warn("read note template", "error", err)`（**不打印内容**）。
- `source` 取值：`fromDefault` 为真时 `"default"`，否则 `"file"`。
- 共用响应构造：`templateResponse(content, fromDefault)` 返回 `map[string]any{"content": content, "path": app.templatePath, "source": source, "default_content": template.Default()}`。
- PUT 校验：`n := utf8.RuneCountInString(body.Content)`；`n > maxContentRunes` → `writeValidationError(w, map[string]string{"content": fmt.Sprintf("模板请控制在 %d 字以内，当前 %d 字", maxContentRunes, n)})`。
- PUT 写盘失败 → `slog.Error("save note template", "error", err)` + `writeError(w, http.StatusInternalServerError, "internal", "无法写入模板文件，请检查配置目录是否可写")`。
- PUT 成功 → `writeJSON(w, http.StatusOK, templateResponse(body.Content, false))`。

- [ ] **Step 5: 注册路由**

`internal/web/server.go` 在 `PUT /api/settings/name` 之后加：

```go
	// 新建笔记的 markdown 骨架：文件在 note.yaml 同目录，GET 只读、PUT 需 CSRF。
	mux.HandleFunc("GET /api/template", app.handleGetTemplate)
	mux.HandleFunc("PUT /api/template", app.handlePutTemplate)
```

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/web -run TestTemplateAPI -v`
Expected: PASS（5 个用例）

- [ ] **Step 7: 提交**

```bash
git add internal/web/handlers_template.go internal/web/template_api_test.go internal/web/server.go internal/web/handlers_settings.go
git commit -m "feat(web): GET/PUT /api/template 读写新建笔记骨架"
```

---

### Task 3: 启动时补一份模板文件

**Files:**

- Modify: `cmd/note/main.go:138-142`（`serve` 里 `slog.Info("starting note", ...)` 之后、`store.Open` 之前）

**Interfaces:**

- Consumes: `template.Ensure` / `template.Path`（Task 1），`config.Config.ConfigPath`（已存在）
- Produces: 启动行为——模板文件缺失时创建，日志一行；无对外接口

- [ ] **Step 1: 加初始化调用**

```go
	// 模板骨架与 note.yaml 同目录：只在缺失时补一份，绝不覆盖用户改过的内容。
	// 配置目录只读时只告警——模板缺失不该让服务起不来。
	templatePath := template.Path(cfg.ConfigPath)
	if created, err := template.Ensure(templatePath); err != nil {
		slog.Warn("prepare note template", "path", templatePath, "error", err)
	} else if created {
		slog.Info("created note template", "path", templatePath)
	}
```

import `note/internal/template`。

- [ ] **Step 2: 编译并跑全量 Go 测试**

Run: `go build ./... && go test ./...`
Expected: 全部通过（`cmd/note` 无测试，编译通过即可）

- [ ] **Step 3: 手工验证前台启动**

Run（Git Bash）：`./note.exe web -c "$(mktemp -d)/note.yaml" --db "$(mktemp -d)/notes.db" -p 8123`
Expected: 输出 `note 已启动`，且 `-c` 指向目录下出现 `note-example.md`；Ctrl+C 后再启动一次，文件内容不变（不覆盖）。验证完删掉临时目录。

- [ ] **Step 4: 提交**

```bash
git add cmd/note/main.go
git commit -m "feat(note): note web 启动时在配置目录补一份 note-example.md"
```

---

### Task 4: 前端模板客户端 `lib/template.ts`

**Files:**

- Create: `web/src/lib/template.ts`
- Test: `web/src/lib/template.test.ts`

**Interfaces:**

- Consumes: `getJSON` / `putJSON` / `ApiRequestError`（`web/src/lib/api.ts`，已存在）；`MAX_CONTENT_CODEPOINTS`（`web/src/lib/notes.ts`，已存在）
- Produces:

  ```ts
  export interface NoteTemplate {
    content: string;
    path: string;
    source: "file" | "default";
    default_content: string;
  }
  export const MAX_TEMPLATE_CODEPOINTS = MAX_CONTENT_CODEPOINTS;
  export function getTemplate(): Promise<NoteTemplate>;
  export function saveTemplate(content: string): Promise<NoteTemplate>;
  ```

- [ ] **Step 1: 写失败的测试**

`web/src/lib/template.test.ts`（照 `web/src/lib/notes.test.ts` 的 fetch mock 写法）：

```ts
import { beforeEach, describe, expect, it, vi } from "vitest";

import { getTemplate, MAX_TEMPLATE_CODEPOINTS, saveTemplate } from "./template";

const sample = {
  content: "# 骨架",
  path: "/home/me/.config/note/note-example.md",
  source: "file",
  default_content: "# 默认",
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("template client", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("GET /api/template", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(sample));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getTemplate()).resolves.toEqual(sample);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/template");
  });

  it("PUT /api/template 带 CSRF 令牌", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ token: "tok" }))
      .mockResolvedValueOnce(jsonResponse(sample));
    vi.stubGlobal("fetch", fetchMock);

    await saveTemplate("# 骨架");

    const [path, init] = fetchMock.mock.calls[1] ?? [];
    expect(path).toBe("/api/template");
    expect((init as RequestInit).method).toBe("PUT");
    expect(JSON.stringify((init as RequestInit).headers)).toContain("tok");
    expect((init as RequestInit).body).toBe(
      JSON.stringify({ content: "# 骨架" }),
    );
  });

  it("400 的字段文案透传到 ApiRequestError.fields", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ token: "tok" }))
      .mockResolvedValueOnce(
        jsonResponse(
          {
            error: {
              code: "validation_failed",
              message: "请修正输入内容",
              fields: { content: "模板请控制在 2000 字以内，当前 2001 字" },
            },
          },
          400,
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(saveTemplate("x")).rejects.toMatchObject({
      code: "validation_failed",
      fields: { content: "模板请控制在 2000 字以内，当前 2001 字" },
    });
  });

  it("模板上限与正文上限一致", () => {
    expect(MAX_TEMPLATE_CODEPOINTS).toBe(2000);
  });
});
```

注：`putJSON` 的内部实现细节（是否先取 CSRF）以 `web/src/lib/api.ts` 现状为准；若该文件的 mock 约定不同，按 `notes.test.ts` 里已验证的写法调整 mock，**但断言（路径、方法、body、错误映射）不得放宽**。

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- --run src/lib/template.test.ts`
Expected: FAIL —— 无法解析 `./template`

- [ ] **Step 3: 写 `web/src/lib/template.ts`**

```ts
// 新建笔记的 markdown 骨架：文件在 note.yaml 同目录，服务端读写，前端只搬运。
import { getJSON, putJSON } from "./api";
import { MAX_CONTENT_CODEPOINTS } from "./notes";

export interface NoteTemplate {
  content: string;
  path: string;
  source: "file" | "default";
  default_content: string;
}

// 模板会原样变成笔记正文，所以上限必须与正文一致。
export const MAX_TEMPLATE_CODEPOINTS = MAX_CONTENT_CODEPOINTS;

export function getTemplate(): Promise<NoteTemplate> {
  return getJSON<NoteTemplate>("/api/template");
}

export function saveTemplate(content: string): Promise<NoteTemplate> {
  return putJSON<NoteTemplate>("/api/template", { content });
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test -- --run src/lib/template.test.ts`
Expected: PASS（4 个用例）

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/template.ts web/src/lib/template.test.ts
git commit -m "feat(web): 模板客户端与 /api/template 对接"
```

---

### Task 5: `/template` 编辑页

**Files:**

- Create: `web/src/views/TemplateView.svelte`
- Modify: `web/src/router/router.ts`（`RouteName` 加 `"template"`，`patterns` 加一条）
- Modify: `web/src/App.svelte`（import、`titles`、视图分支）
- Modify: `web/src/app.css`（模板页布局，尽量复用 `.editor-grid` / `.preview` / `.page`）
- Test: `web/src/router/router.test.ts`（补一条断言）

**Interfaces:**

- Consumes: `getTemplate` / `saveTemplate` / `MAX_TEMPLATE_CODEPOINTS`（Task 4）；`Markdown.svelte` 的 `source: string` prop；`countCodePoints`（`lib/notes.ts`）；`toast`（`lib/toast.ts`）；`navigate`（`router/router.ts`）
- Produces: 路由名 `"template"`，路径 `/template`，支持 `?next=<path>` 返回目标

- [ ] **Step 1: 补路由（先写断言）**

`web/src/router/router.test.ts` 里加：`expect(matchRoute("/template")).toEqual({ name: "template", params: {} })`，以及 `matchRoute("/template?next=%2Fnotes%2Fnew").name === "template"`。

Run: `npm --prefix web test -- --run src/router/router.test.ts`
Expected: FAIL —— 返回 `notFound`

- [ ] **Step 2: 改 `web/src/router/router.ts`**

`RouteName` 联合类型加 `| "template"`（放在 `"info"` 之后），`patterns` 数组加：

```ts
  { name: "template", pattern: /^\/template$/, keys: [] },
```

Run: `npm --prefix web test -- --run src/router/router.test.ts`
Expected: PASS

- [ ] **Step 3: 写 `web/src/views/TemplateView.svelte`**

结构与 `NoteEditorView` 的预览两栏一致：

- 返回目标自行从 URL 读取，不做 prop：`const next = new URLSearchParams(location.search).get('next') || '/notes/new'`（在挂载时读一次即可——每次进入该路由都会重建组件）。
- 状态：`content`（textarea）、`original`（载入时的内容，用于判断 dirty）、`path`、`source`、`defaultContent`、`error`、`notice`、`busy`、`loading`、`previewSource`。
- 挂载时 `await getTemplate()`：填 `content`/`original`/`path`/`source`/`defaultContent`；失败时 `error = 错误文案` 并允许重试。
- 预览防抖 300ms（与编辑器同款 `$effect`）。
- 顶部：`<h1>新建笔记模板</h1>` + 一行 `muted` 显示 `文件：{path}`；`source === 'default'` 时额外提示「模板文件还没创建，显示的是内置默认内容；保存后会写回该文件。」
- 计数器 `{countCodePoints(content)} / {MAX_TEMPLATE_CODEPOINTS}`，超限时 `counter-over` 且禁用保存。
- 按钮组：`保存模板`（`disabled={busy || loading || tooLong || content === original}`）、`恢复默认`（把 `defaultContent` 填进 textarea，**不落盘**，并把 `notice` 置为「已填入内置默认内容，点『保存模板』才会写回文件。」）、`返回`。
- 保存：`const saved = await saveTemplate(content)`，成功后 `original = saved.content`、`path = saved.path`、`source = "file"`，`toast('模板已保存')`；失败时若 `ApiRequestError.fields?.content` 有值就显示为主错误文案，否则 `error = 错误消息`。
- 返回：`navigate(next)`。

- [ ] **Step 4: 接进 `App.svelte`**

- `import TemplateView from './views/TemplateView.svelte'`
- `titles` 加 `template: '新建笔记模板'`
- 视图分支加：

```svelte
    {:else if $route.name === 'template'}
      <TemplateView />
```

- [ ] **Step 5: 加样式**

`web/src/app.css` 复用 `.editor-grid`（两栏）+ `.preview`；只新增模板页特有的顶部路径行（`.template-path`，`font-family: var(--font-mono)`、`color: var(--muted)`）与按钮组容器（复用现有 `.actions`）。窄屏断点沿用 `.editor-grid` 已有的单列规则，不要新写媒体查询。

- [ ] **Step 6: 手测渲染**

Run: `just start`，打开 `http://127.0.0.1:5173/template`
Expected: 能看到默认骨架与右侧渲染；改内容后计数器变化；点「恢复默认」只回填不落盘；点「保存模板」后提示成功、文件路径仍在；在 textarea 里写 `<script>alert(1)</script>` 与 `[x](javascript:alert(1))`，右侧预览只得纯文本（不弹窗、无 `javascript:` 链接）；浏览器控制台无报错。

- [ ] **Step 7: 提交**

```bash
git add web/src/views/TemplateView.svelte web/src/router web/src/App.svelte web/src/app.css
git commit -m "feat(web): /template 页面编辑新建笔记骨架"
```

---

### Task 6: 新建笔记预填骨架 + 「编辑模板」入口

**Files:**

- Modify: `web/src/views/NoteEditorView.svelte`

**Interfaces:**

- Consumes: `getTemplate`（Task 4）；`editorDraft`（`lib/draft.ts`，已存在）；路由 `/template?next=…`（Task 5）
- Produces: 行为——新建笔记正文预填模板；`editorDraft` 交棒后返回仍能看到草稿

- [ ] **Step 1: 预填正文**

`loadFormChrome()` 里拿到 `cats`/`master` 之后追加（保持在 `if (isEdit) await loadNote()` 之前）：

```ts
// 新建笔记预填模板骨架；有草稿时草稿优先（draft 在 $effect 里已先行填入）。
if (!isEdit && content === "") {
  try {
    const tpl = await getTemplate();
    if (content === "") content = tpl.content;
  } catch {
    // 模板拿不到就是空正文：编辑器不能因此打不开。
  }
}
```

`dirty` 保持 `false`（骨架不是用户输入，不该触发 `beforeunload`）。

- [ ] **Step 2: 加「编辑模板」入口**

标题行的 `field-label-row` 内、计数器之前加（**仅新建时显示**）：

```svelte
          {#if !isEdit}
            <button class="linklike" type="button" onclick={editTemplate}>编辑模板</button>
          {/if}
```

新增函数（与 `goToMasterPassword` 同一套交棒机制）：

```ts
// 跳去改模板属于「交棒」：先把当前表单存成草稿，返回时接着编。
function editTemplate(): void {
  handingOff = true;
  editorDraft.save({ noteId: id, title, content, categoryId, encrypt });
  navigate(`/template?next=${encodeURIComponent("/notes/new")}`);
}
```

- [ ] **Step 3: 手测三条路径**

Run: `just start`，然后：

1. 打开 `/notes/new` → 正文已是骨架、右侧预览有渲染、计数器非 0、标题为空。
2. 填标题「测试」+ 改正文，点「编辑模板」→ 到 `/template`；改模板保存后点「返回」→ 回到 `/notes/new`，**标题与正文草稿都还在**，并提示已恢复草稿。
3. 保存这条笔记 → 列表里出现，正文是改过的骨架内容。

Expected: 三条全部符合；浏览器控制台无报错。

- [ ] **Step 4: 提交**

```bash
git add web/src/views/NoteEditorView.svelte
git commit -m "feat(web): 新建笔记预填 markdown 骨架，并提供编辑模板入口"
```

---

### Task 7: 文档、测试全跑、CHANGELOG

**Files:**

- Modify: `docs/TECH_DESIGN.md`（4.2 文件位置规则、4.3 路由清单、8.3 端点表、目录树里加 `internal/template/` 与 `views/TemplateView.svelte`）
- Modify: `docs/PRD.md`（API 表加 `/api/template` 两行；能力/约束表里补一条模板能力）
- Modify: `CHANGELOG.md`（`[Unreleased]` → `### Added`）

**Interfaces:**

- Consumes: 前六个任务的最终代码
- Produces: 文档与变更记录

- [ ] **Step 1: 跑全量测试与检查**

Run: `just test`
Expected: Go 全部包 PASS，前端 Vitest 全部 PASS（含新增 4 + 4 个用例）

Run: `just check`
Expected: `go vet` 无输出，`svelte-check` 0 error 0 warning

- [ ] **Step 2: 更新 `docs/TECH_DESIGN.md`**

- 4.2 配置一节补一条规则：模板文件路径由纯函数 `template.Path(cfg.ConfigPath)` 推导（`dir(note.yaml)/note-example.md`），`note web` 启动时缺失则写入内置默认值，**已存在不覆盖**，配置目录不可写只告警；权限 `0644`。
- 4.3 路由清单加：
  ```
  GET    /api/template              # 新建笔记的 markdown 骨架
  PUT    /api/template              # 覆盖骨架文件（需 CSRF）
  ```
- 8.3 端点表加两行（GET 无请求、200 `{"content","path","source","default_content"}`；PUT `{"content":"…"}`，> 2000 码点 400 `validation_failed`）。
- 目录结构里补 `internal/template/`（`template.go` + `default.md`）与 `web/src/views/TemplateView.svelte`、`web/src/lib/template.ts`。

- [ ] **Step 3: 更新 `docs/PRD.md`**

API 表加 `/api/template` 两行；在编辑器相关能力/约束条目里加一句：新笔记正文预填用户可编辑的 markdown 骨架（`note-example.md`，与 `note.yaml` 同目录，页面 `/template` 可改，恢复默认不直接落盘）。

- [ ] **Step 4: 写 `CHANGELOG.md`**

`[Unreleased]` → `### Added` 顶部插入一条，按本项目既有写法记录：做法（`internal/template` 包 + `go:embed` + 启动补文件 + `GET/PUT /api/template` + `/template` 页面 + 编辑器预填与交棒）、取舍（不覆盖已有文件、2000 码点上限、只读目录只告警、恢复默认不落盘）、要改的文件、测试与验证结论（**只写实际跑出来的数字与结果**）。

- [ ] **Step 5: 格式化并提交**

```bash
just format
git add docs/TECH_DESIGN.md docs/PRD.md CHANGELOG.md docs/superpowers
git commit -m "docs: 补充新建笔记模板的能力、端点与变更记录"
```

---

## 交付前自查

- `just test` 与 `just check` 全绿（AGENTS.md 硬性要求）。
- `git status` 里除本计划的文件外没有意外改动（尤其不要提交 `.run/`、`note.exe`）。
- 手测第 3、6 任务的验证路径都在真实浏览器里点过一遍。
