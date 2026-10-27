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
