package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const testKDFParams = `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":150000,"keylen":32}`

func noteTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

// configureMaster 写入主密码配置：加密笔记落库需要它作为 kdf_salt 副本来源。
func configureMaster(t *testing.T, st *Store) {
	t.Helper()

	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(0xf0 + i)
	}
	if _, err := st.SetMasterPassword(context.Background(), salt, "你的生日是多少？"); err != nil {
		t.Fatalf("configure master password: %v", err)
	}
}

func testCipher() []byte {
	return []byte(strings.Repeat("0123456789abcdef", 2)) // 32 字节
}

func TestNewNoteIDFormat(t *testing.T) {
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	a, b := NewNoteID(), NewNoteID()
	if !uuid.MatchString(a) || !uuid.MatchString(b) {
		t.Fatalf("not uuid v4: %q %q", a, b)
	}
	if a == b {
		t.Fatal("expected distinct ids")
	}
}

func TestCreateNotePlaintextAndEncrypted(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	body := "正文"

	plain, err := st.CreateNote(ctx, NoteInput{Title: "T1", Content: &body})
	if err != nil {
		t.Fatalf("create plain note: %v", err)
	}
	if plain.Kind != "note" || plain.Rev != 1 || plain.CreatedAt != plain.UpdatedAt {
		t.Fatalf("unexpected defaults: %+v", plain)
	}
	if plain.Content == nil || *plain.Content != body {
		t.Fatalf("content mismatch: %+v", plain.Content)
	}
	if plain.Encrypted || plain.CipherB64 != "" {
		t.Fatalf("plain note must not carry cipher: %+v", plain)
	}

	var cipherColumnsNull bool
	if err := st.db.QueryRow(
		`SELECT cipher IS NULL AND kdf_salt IS NULL AND kdf_params IS NULL FROM notes WHERE id = ?`, plain.ID,
	).Scan(&cipherColumnsNull); err != nil {
		t.Fatalf("query cipher columns: %v", err)
	}
	if !cipherColumnsNull {
		t.Fatal("expected cipher/kdf_salt/kdf_params to be NULL for plaintext note")
	}

	cipher := testCipher() // nonce(12) + 密文 + tag(16) 的最小自洽形态
	if _, err := st.CreateNote(ctx, NoteInput{
		Title:     "Secret",
		CipherB64: base64.StdEncoding.EncodeToString(cipher),
		KDFParams: testKDFParams,
	}); !errors.Is(err, ErrMasterPasswordMissing) {
		t.Fatalf("expected encrypted create to require master password, got %v", err)
	}

	configureMaster(t, st)
	enc, err := st.CreateNote(ctx, NoteInput{
		Title:     "Secret",
		CipherB64: base64.StdEncoding.EncodeToString(cipher),
		KDFParams: testKDFParams,
	})
	if err != nil {
		t.Fatalf("create encrypted note: %v", err)
	}
	if !enc.Encrypted {
		t.Fatalf("expected encrypted flag: %+v", enc)
	}

	back, err := st.GetNote(ctx, enc.ID)
	if err != nil {
		t.Fatalf("get encrypted note: %v", err)
	}
	if back.Content != nil {
		t.Fatalf("encrypted note must have NULL content, got %q", *back.Content)
	}
	got, err := base64.StdEncoding.DecodeString(back.CipherB64)
	if err != nil {
		t.Fatalf("decode cipher_b64: %v", err)
	}
	if string(got) != string(cipher) {
		t.Fatalf("cipher bytes differ: %x", got)
	}
	if back.KDFParams == nil || *back.KDFParams != testKDFParams {
		t.Fatalf("kdf_params mismatch: %+v", back.KDFParams)
	}

	var contentNull bool
	if err := st.db.QueryRow(
		`SELECT content IS NULL FROM notes WHERE id = ?`, enc.ID,
	).Scan(&contentNull); err != nil {
		t.Fatalf("query content column: %v", err)
	}
	if !contentNull {
		t.Fatal("expected stored content to be NULL for encrypted note")
	}

	// kdf_salt 落的是 master_password.salt 的冗余副本（只为满足 0001 的 CHECK；解密一律以 master_password.salt 为准）
	var kdfSalt []byte
	if err := st.db.QueryRow(`SELECT kdf_salt FROM notes WHERE id = ?`, enc.ID).Scan(&kdfSalt); err != nil {
		t.Fatalf("query kdf_salt: %v", err)
	}
	master, _, err := st.GetMasterPassword(ctx)
	if err != nil {
		t.Fatalf("get master password: %v", err)
	}
	want, err := base64.StdEncoding.DecodeString(master.SaltB64)
	if err != nil {
		t.Fatalf("decode master salt: %v", err)
	}
	if string(kdfSalt) != string(want) {
		t.Fatalf("kdf_salt must mirror master salt: %x vs %x", kdfSalt, want)
	}
}

func TestNotePlaintextToEncryptedTransition(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	configureMaster(t, st)
	body := "可升加密"

	note, err := st.CreateNote(ctx, NoteInput{Title: "T", Content: &body})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cipherB64 := base64.StdEncoding.EncodeToString(testCipher())
	upd, err := st.UpdateNote(ctx, note.ID, NoteInput{Title: "T", CipherB64: cipherB64, KDFParams: testKDFParams})
	if err != nil {
		t.Fatalf("encrypt existing note: %v", err)
	}
	if !upd.Encrypted || upd.Content != nil {
		t.Fatalf("expected encrypted note without content: %+v", upd)
	}

	// 不可逆：加密态回明文会被 0001 的 CHECK 拒绝
	if _, err := st.UpdateNote(ctx, note.ID, NoteInput{Title: "T", Content: &body}); err == nil {
		t.Fatal("expected decrypt-to-plaintext update to be rejected")
	}
}

func TestUpdateAndDeleteNote(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	body := "v1"

	note, err := st.CreateNote(ctx, NoteInput{Title: "标题", Content: &body})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	v2 := "v2"
	updated, err := st.UpdateNote(ctx, note.ID, NoteInput{Title: "标题2", Content: &v2})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Rev != 2 || updated.Title != "标题2" {
		t.Fatalf("unexpected revision or title after update: %+v", updated)
	}
	if updated.CreatedAt != note.CreatedAt {
		t.Fatalf("created_at must not change: %q vs %q", updated.CreatedAt, note.CreatedAt)
	}

	if _, err := st.UpdateNote(ctx, "missing-id", NoteInput{Title: "x", Content: &v2}); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on update, got %v", err)
	}
	if err := st.DeleteNote(ctx, "missing-id"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on delete, got %v", err)
	}

	if err := st.DeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetNote(ctx, note.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on get, got %v", err)
	}
}

func TestListNotesPaging(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	for i := 0; i < 45; i++ {
		body := "body"
		in := NoteInput{Title: string(rune('a' + i%26)) + " 标题", Content: &body}
		if i == 0 {
			// 显式指定 id：CreateNote 接受客户端生成的 id（Task 10 编辑器需要）
			in.ID = NewNoteID()
		}
		if _, err := st.CreateNote(ctx, in); err != nil {
			t.Fatalf("create note %d: %v", i, err)
		}
	}

	page1, err := st.ListNotes(ctx, "", 0, 1)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(page1.Items) != 20 || page1.Total != 45 || page1.Page != 1 || page1.PerPage != 20 {
		t.Fatalf("unexpected page 1: %d items, total %d", len(page1.Items), page1.Total)
	}
	page3, err := st.ListNotes(ctx, "", 0, 3)
	if err != nil {
		t.Fatalf("list page 3: %v", err)
	}
	if len(page3.Items) != 5 {
		t.Fatalf("expected 5 items on page 3, got %d", len(page3.Items))
	}
	for i := 1; i < len(page1.Items); i++ {
		if page1.Items[i-1].UpdatedAt < page1.Items[i].UpdatedAt {
			t.Fatal("expected updated_at DESC ordering")
		}
	}
}

func TestListNotesSearchOrdersTitleHitsFirst(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "alpha"
	tail := " 尾部正文"
	if _, err := st.CreateNote(ctx, NoteInput{Title: "普通笔记", Content: &body}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.CreateNote(ctx, NoteInput{Title: "会议记录", Content: &tail}); err != nil {
		t.Fatalf("create: %v", err)
	}

	page, err := st.ListNotes(ctx, "会议", 0, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) == 0 || page.Items[0].Title != "会议记录" {
		t.Fatalf("expected title hit first, got %+v", page.Items)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM activities WHERE query = '会议'`).Scan(&count); err != nil {
		t.Fatalf("count activities: %v", err)
	}
	if count == 0 {
		t.Fatal("expected ListNotes with q to record one activity")
	}
}

func TestListNotesSearchTreatsLikeWildcardsAsLiterals(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	for _, title := range []string{"100% 完成", "a_b 下划线", "反斜杠 \\ 用法", "普通笔记"} {
		if _, err := st.CreateNote(ctx, NoteInput{Title: title, Content: &body}); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
	}

	// % 与 _ 是 LIKE 的通配符，没转义就会把「100% 完成」这类查询扩散成全部笔记
	for _, tc := range []struct {
		q    string
		want int
	}{
		{"%", 1},
		{"_", 1},
		{`\`, 1},
		{"100% 完成", 1},
		{"a_b", 1},
		{"普通", 1},
	} {
		page, err := st.ListNotes(ctx, tc.q, 0, 1)
		if err != nil {
			t.Fatalf("list %q: %v", tc.q, err)
		}
		if page.Total != tc.want || len(page.Items) != tc.want {
			t.Fatalf("query %q: expected %d hit, got total %d items %+v", tc.q, tc.want, page.Total, page.Items)
		}
	}
}

func TestListNotesCategoryFilterAndEmptyQuery(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	work, err := st.CreateCategory(ctx, "工作")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	body := "正文"
	cat := work.ID
	if _, err := st.CreateNote(ctx, NoteInput{Title: "周报", Content: &body, CategoryID: &cat}); err != nil {
		t.Fatalf("create categorized note: %v", err)
	}
	if _, err := st.CreateNote(ctx, NoteInput{Title: "购物", Content: &body}); err != nil {
		t.Fatalf("create uncategorized note: %v", err)
	}

	page, err := st.ListNotes(ctx, "", work.ID, 1)
	if err != nil {
		t.Fatalf("list by category: %v", err)
	}
	if len(page.Items) != 1 || page.Total != 1 || page.Items[0].Title != "周报" {
		t.Fatalf("unexpected filtered page: %+v", page.Items)
	}
	if page.Items[0].CategoryName != "工作" {
		t.Fatalf("expected category name, got %q", page.Items[0].CategoryName)
	}
	if page.Items[0].Content == nil {
		t.Fatal("expected summary content for plaintext note")
	}

	all, err := st.ListNotes(ctx, "", 0, 1)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all.Items) != 2 {
		t.Fatalf("expected 2 notes unfiltered, got %d", len(all.Items))
	}
}

func TestNoteContentUnicodeCounting(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := strings.Repeat("🚀", 2000)
	created, err := st.CreateNote(ctx, NoteInput{Title: "emoji", Content: &body})
	if err != nil {
		t.Fatalf("create note with 2000 emoji: %v", err)
	}

	back, err := st.GetNote(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if back.Content == nil || len([]rune(*back.Content)) != 2000 {
		t.Fatal("expected 2000-rune content stored and read back intact")
	}

	long := strings.Repeat("字", 2001)
	if _, err := st.CreateNote(ctx, NoteInput{Title: "over", Content: &long}); err == nil {
		t.Fatal("expected store to reject content over 2000 runes")
	}
}

func TestCreateNoteValidatesStructure(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	// 既无正文也无密文：结构非法
	if _, err := st.CreateNote(ctx, NoteInput{Title: "空"}); !errors.Is(err, sql.ErrNoRows) && err == nil {
		t.Fatal("expected empty note body to be rejected")
	}
	// 同时给正文与密文：非法
	body := "x"
	both := NoteInput{Title: "两者", Content: &body, CipherB64: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("y", 32))), KDFParams: testKDFParams}
	if _, err := st.CreateNote(ctx, both); err == nil {
		t.Fatal("expected plaintext+cipher note to be rejected")
	}
	// 密文缺参数：非法
	if _, err := st.CreateNote(ctx, NoteInput{Title: "缺参数", CipherB64: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("y", 32)))}); err == nil {
		t.Fatal("expected cipher without kdf_params to be rejected")
	}
	// 未知分类：外键拒绝
	ghost := int64(9999)
	if _, err := st.CreateNote(ctx, NoteInput{Title: "未知分类", Content: &body, CategoryID: &ghost}); err == nil {
		t.Fatal("expected unknown category to be rejected")
	}
	// 客户端指定 id 被采纳
	fixed := NewNoteID()
	note, err := st.CreateNote(ctx, NoteInput{ID: fixed, Title: "指定 id", Content: &body})
	if err != nil {
		t.Fatalf("create with explicit id: %v", err)
	}
	if note.ID != fixed {
		t.Fatalf("expected explicit id %q, got %q", fixed, note.ID)
	}
	// 重复 id 报错而非静默覆盖
	if _, err := st.CreateNote(ctx, NoteInput{ID: fixed, Title: "重复", Content: &body}); err == nil {
		t.Fatal("expected duplicate id to be rejected")
	}
}

// createEncryptedNote 建一条加密笔记，供改密迁移的清单与替换测试复用。
func createEncryptedNote(t *testing.T, st *Store, ctx context.Context, title string, categoryID *int64) Note {
	t.Helper()

	note, err := st.CreateNote(ctx, NoteInput{
		Title:      title,
		CipherB64:  base64.StdEncoding.EncodeToString(testCipher()),
		KDFParams:  testKDFParams,
		CategoryID: categoryID,
	})
	if err != nil {
		t.Fatalf("create encrypted note %q: %v", title, err)
	}
	return note
}

func TestListEncryptedCiphers(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	configureMaster(t, st)

	body := "这条不该出现在清单里"
	if _, err := st.CreateNote(ctx, NoteInput{Title: "明文", Content: &body}); err != nil {
		t.Fatalf("create plaintext note: %v", err)
	}
	first := createEncryptedNote(t, st, ctx, "第一条", nil)
	second := createEncryptedNote(t, st, ctx, "第二条", nil)

	items, err := st.ListEncryptedCiphers(ctx)
	if err != nil {
		t.Fatalf("list encrypted ciphers: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected only the 2 encrypted rows, got %+v", items)
	}

	wantCipher := base64.StdEncoding.EncodeToString(testCipher())
	seen := map[string]EncryptedCipher{}
	for _, item := range items {
		seen[item.ID] = item
	}
	for _, id := range []string{first.ID, second.ID} {
		item, ok := seen[id]
		if !ok {
			t.Fatalf("missing %q in %+v", id, items)
		}
		if item.CipherB64 != wantCipher {
			t.Errorf("cipher_b64 of %q = %q, want %q", id, item.CipherB64, wantCipher)
		}
		if item.KDFParams != testKDFParams {
			t.Errorf("kdf_params of %q = %q, want原样回显 %q", id, item.KDFParams, testKDFParams)
		}
	}
}

func TestListEncryptedCiphersEmpty(t *testing.T) {
	st := noteTestStore(t)

	items, err := st.ListEncryptedCiphers(context.Background())
	if err != nil {
		t.Fatalf("list encrypted ciphers on empty store: %v", err)
	}
	// 前端直接 JSON.stringify，nil 会输出 null 而不是空数组
	if items == nil {
		t.Fatal("expected non-nil empty slice so the API emits []")
	}
	if len(items) != 0 {
		t.Fatalf("expected no rows, got %+v", items)
	}
}

func TestUpdateNoteCipherKeepsMetadata(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	master, err := st.SetMasterPassword(ctx, make([]byte, masterSaltLen), "你的生日是多少？")
	if err != nil {
		t.Fatalf("configure master password: %v", err)
	}
	category, err := st.CreateCategory(ctx, "工作")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	note := createEncryptedNote(t, st, ctx, "改密前", &category.ID)
	newCipher := append(append([]byte{}, testCipher()...), 'x')
	newParams := `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":200000,"keylen":32}`

	updated, err := st.UpdateNoteCipher(ctx, note.ID, base64.StdEncoding.EncodeToString(newCipher), newParams)
	if err != nil {
		t.Fatalf("update note cipher: %v", err)
	}

	if updated.CipherB64 != base64.StdEncoding.EncodeToString(newCipher) {
		t.Errorf("cipher_b64 = %q, want the new cipher", updated.CipherB64)
	}
	if updated.KDFParams == nil || *updated.KDFParams != newParams {
		t.Errorf("kdf_params = %v, want it refreshed to %q", updated.KDFParams, newParams)
	}
	if !updated.Encrypted {
		t.Error("note must stay encrypted")
	}
	// 改密不是用户可见的修订：标题、分类、rev 与两个时间戳都不许动
	if updated.Title != note.Title {
		t.Errorf("title = %q, want %q", updated.Title, note.Title)
	}
	if updated.CategoryID == nil || *updated.CategoryID != category.ID {
		t.Errorf("category_id = %v, want %d", updated.CategoryID, category.ID)
	}
	if updated.Rev != note.Rev {
		t.Errorf("rev = %d, want it unchanged at %d", updated.Rev, note.Rev)
	}
	if updated.CreatedAt != note.CreatedAt || updated.UpdatedAt != note.UpdatedAt {
		t.Errorf("timestamps changed: created %q->%q updated %q->%q",
			note.CreatedAt, updated.CreatedAt, note.UpdatedAt, updated.UpdatedAt)
	}

	var salt []byte
	if err := st.db.QueryRowContext(ctx, `SELECT kdf_salt FROM notes WHERE id = ?`, note.ID).Scan(&salt); err != nil {
		t.Fatalf("read kdf_salt: %v", err)
	}
	if base64.StdEncoding.EncodeToString(salt) != master.SaltB64 {
		t.Errorf("kdf_salt copy must track master_password.salt")
	}
}

func TestUpdateNoteCipherRejectsPlaintextAndMissing(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	body := "明文笔记"
	plain, err := st.CreateNote(ctx, NoteInput{Title: "明文", Content: &body})
	if err != nil {
		t.Fatalf("create plaintext note: %v", err)
	}

	newCipher := base64.StdEncoding.EncodeToString(testCipher())
	if _, err := st.UpdateNoteCipher(ctx, plain.ID, newCipher, testKDFParams); !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("plaintext note: err = %v, want ErrNotEncrypted", err)
	}

	ghost := NewNoteID()
	if _, err := st.UpdateNoteCipher(ctx, ghost, newCipher, testKDFParams); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNoteNotFound", err)
	}

	if _, err := st.UpdateNoteCipher(ctx, plain.ID, base64.StdEncoding.EncodeToString([]byte("short")), testKDFParams); !errors.Is(err, ErrInvalidNote) {
		t.Errorf("too short cipher: err = %v, want ErrInvalidNote", err)
	}
}
