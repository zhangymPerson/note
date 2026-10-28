package daemon

import (
	"os"
	"testing"
)

func TestProcessAliveForSelf(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatalf("自身 pid %d 被判为不存在", os.Getpid())
	}
}

func TestProcessAliveForImpossiblePid(t *testing.T) {
	if ProcessAlive(1 << 30) {
		t.Fatal("pid 1073741824 被判为存活")
	}
}

func TestProcessAliveForZeroOrNegative(t *testing.T) {
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Fatal("0 / 负数 pid 不应判为存活")
	}
}
