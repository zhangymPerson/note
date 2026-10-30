package clipboard

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// stub 换掉两条写路径，返回一个记录 OSC52 输出的缓冲区。
func stub(t *testing.T, nativeErr error, oscTTY bool) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previousWrite, previousOut, previousSupported := writeNative, oscOut, oscSupported
	t.Cleanup(func() {
		writeNative, oscOut, oscSupported = previousWrite, previousOut, previousSupported
	})
	writeNative = func(string) error { return nativeErr }
	oscOut = &out
	oscSupported = func() bool { return oscTTY }
	return &out
}

func TestCopyPrefersNative(t *testing.T) {
	out := stub(t, nil, true)

	if err := Copy("正文"); err != nil {
		t.Fatalf("原生成功时不该报错：%v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("原生成功时不该再发 OSC52：%q", out.String())
	}
}

func TestCopyFallsBackToOSC52(t *testing.T) {
	out := stub(t, errors.New("no xclip"), true)

	if err := Copy("第一行\n第二行"); err != nil {
		t.Fatalf("OSC52 兜底成功时不该报错：%v", err)
	}

	sequence := out.String()
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("第一行\n第二行")) + "\x1b\\"
	if sequence != want {
		t.Fatalf("OSC52 序列不对：\n got %q\nwant %q", sequence, want)
	}
}

func TestCopyFailsWhenNotTerminal(t *testing.T) {
	out := stub(t, errors.New("no xclip"), false)

	err := Copy("正文")
	if err == nil {
		t.Fatal("两条路都不可用时应当报错")
	}
	if !strings.Contains(err.Error(), "no xclip") || !strings.Contains(err.Error(), "stdout") {
		t.Fatalf("错误信息应当同时说明两条路为什么失败：%v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("非终端不该把转义序列漏进输出：%q", out.String())
	}
}

func TestWriteOSC52RejectsOversizedText(t *testing.T) {
	out := stub(t, errors.New("no xclip"), true)

	big := strings.Repeat("a", maxOSC52Bytes+1)
	if err := Copy(big); err == nil {
		t.Fatal("超过 OSC52 上限时应当报错")
	}
	if out.Len() != 0 {
		t.Fatalf("超限时不该写序列：%d 字节", out.Len())
	}
}

func TestExplainFailure(t *testing.T) {
	if got := ExplainFailure(nil); got != "" {
		t.Fatalf("nil 应当返回空串，得到 %q", got)
	}
	got := ExplainFailure(errors.New("exit status 1"))
	if !strings.HasPrefix(got, "复制到剪贴板失败：") || !strings.Contains(got, "exit status 1") {
		t.Fatalf("提示文案不对：%q", got)
	}
}
