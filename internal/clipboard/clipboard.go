// Package clipboard 把文本写进系统剪贴板。
//
// 两条路，先试后一条：
//  1. 原生实现（github.com/atotto/clipboard）：Windows 走 clip.exe、macOS 走 pbcopy、
//     Linux 走 xclip/xsel。本机最可靠，但在 SSH 会话或没装 xclip 的机器上会失败。
//  2. OSC52 转义序列：把内容 base64 后交给终端自己复制。SSH 与容器里也能用，
//     前提是终端支持（Windows Terminal、iTerm2、wezterm、kitty、alacritty 等）。
//
// 两条都失败就返回错误，由调用方决定提示与否——复制失败绝不该让「看笔记」失败。
package clipboard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	atotto "github.com/atotto/clipboard"
	"github.com/charmbracelet/x/term"
)

// maxOSC52Bytes 是 OSC52 载荷上限（原文，非 base64）：终端普遍对整条转义序列
// 有 100 KiB 量级的限制，超过就不试了，免得把一大段序列糊进终端。
const maxOSC52Bytes = 100 << 10

// writeNative 与 oscOut 留成变量，测试可以直接替换，不必真的碰剪贴板。
var (
	writeNative            = atotto.WriteAll
	oscOut       io.Writer = os.Stdout
	oscSupported           = func() bool { return term.IsTerminal(os.Stdout.Fd()) }
)

// Copy 把 text 写进剪贴板。空文本也照写（清空剪贴板是合法意图）。
func Copy(text string) error {
	nativeErr := writeNative(text)
	if nativeErr == nil {
		return nil
	}

	oscErr := writeOSC52(oscOut, text)
	if oscErr == nil {
		return nil
	}
	return errors.Join(nativeErr, oscErr)
}

// writeOSC52 输出 OSC 52 复制序列：ESC ] 52 ; c ; <base64> ESC \。
// stdout 不是终端（管道、重定向）时不写：那会把转义序列漏进别人的文件。
func writeOSC52(out io.Writer, text string) error {
	if !oscSupported() {
		return errors.New("stdout 不是终端，无法用 OSC52 复制")
	}
	if len(text) > maxOSC52Bytes {
		return fmt.Errorf("正文 %d 字节，超过 OSC52 的 %d 字节上限", len(text), maxOSC52Bytes)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if _, err := io.WriteString(out, "\x1b]52;c;"+encoded+"\x1b\\"); err != nil {
		return fmt.Errorf("写 OSC52 序列失败：%w", err)
	}
	return nil
}

// ExplainFailure 把复制失败的原因压成一句给人看的提示，去掉包名与嵌套细节。
func ExplainFailure(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "复制到剪贴板失败"
	}
	return "复制到剪贴板失败：" + message
}
