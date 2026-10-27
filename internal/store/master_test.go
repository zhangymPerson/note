package store

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestMasterPasswordSetOnce(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if got, ok, err := st.GetMasterPassword(ctx); err != nil || ok || got != (MasterPassword{}) {
		t.Fatalf("expected unconfigured, got %+v ok=%v err=%v", got, ok, err)
	}

	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i + 1)
	}

	created, err := st.SetMasterPassword(ctx, salt, "你的生日是多少？")
	if err != nil {
		t.Fatalf("set master password: %v", err)
	}
	if created.SaltB64 != base64.StdEncoding.EncodeToString(salt) {
		t.Fatalf("unexpected salt echo: %q", created.SaltB64)
	}
	if created.Question != "你的生日是多少？" {
		t.Fatalf("unexpected question: %q", created.Question)
	}
	if created.CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}

	got, ok, err := st.GetMasterPassword(ctx)
	if err != nil || !ok {
		t.Fatalf("get configured master password: ok=%v err=%v", ok, err)
	}
	if got != created {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, created)
	}

	again, err := st.SetMasterPassword(ctx, salt, "另一个问题")
	if !errors.Is(err, ErrMasterPasswordExists) {
		t.Fatalf("expected ErrMasterPasswordExists, got %v", err)
	}
	if again != (MasterPassword{}) {
		t.Fatalf("expected zero value on rejected set, got %+v", again)
	}

	got, _, err = st.GetMasterPassword(ctx)
	if err != nil {
		t.Fatalf("get after rejected set: %v", err)
	}
	if got.Question != "你的生日是多少？" {
		t.Fatalf("second set must not overwrite, got %+v", got)
	}
}

func TestMasterPasswordRejectsBadSalt(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := st.SetMasterPassword(ctx, make([]byte, 15), "问题"); err == nil {
		t.Fatal("expected 15-byte salt to be rejected")
	}
	if _, err := st.SetMasterPassword(ctx, make([]byte, 32), "问题"); err == nil {
		t.Fatal("expected 32-byte salt to be rejected")
	}

	if _, ok, err := st.GetMasterPassword(ctx); err != nil || ok {
		t.Fatalf("expected still unconfigured, ok=%v err=%v", ok, err)
	}
}

func TestUpdateMasterQuestion(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := st.UpdateMasterQuestion(ctx, "第一份工作？"); !errors.Is(err, ErrMasterPasswordMissing) {
		t.Errorf("未配置时: err = %v, want ErrMasterPasswordMissing", err)
	}

	master, err := st.SetMasterPassword(ctx, make([]byte, masterSaltLen), "你的生日是多少？")
	if err != nil {
		t.Fatalf("set master password: %v", err)
	}

	updated, err := st.UpdateMasterQuestion(ctx, "  第一份工作？  ")
	if err != nil {
		t.Fatalf("update question: %v", err)
	}
	if updated.Question != "第一份工作？" {
		t.Errorf("question = %q, want 首尾空白被裁掉的 %q", updated.Question, "第一份工作？")
	}

	got, ok, err := st.GetMasterPassword(ctx)
	if err != nil || !ok {
		t.Fatalf("get master password: ok=%v err=%v", ok, err)
	}
	if got.Question != "第一份工作？" {
		t.Errorf("persisted question = %q", got.Question)
	}
	// 改助记问题绝不换盐，也不动创建时间——否则未迁移的笔记当场不可解
	if got.SaltB64 != master.SaltB64 {
		t.Errorf("salt changed: %q -> %q", master.SaltB64, got.SaltB64)
	}
	if got.CreatedAt != master.CreatedAt {
		t.Errorf("created_at changed: %q -> %q", master.CreatedAt, got.CreatedAt)
	}

	for _, blank := range []string{"", "   ", "\t\n"} {
		if _, err := st.UpdateMasterQuestion(ctx, blank); err == nil {
			t.Errorf("空白问题 %q 必须被拒绝", blank)
		}
	}
	kept, _, err := st.GetMasterPassword(ctx)
	if err != nil {
		t.Fatalf("get after rejections: %v", err)
	}
	if kept.Question != "第一份工作？" {
		t.Errorf("被拒绝的写入不得改动原问题，got %q", kept.Question)
	}
}
