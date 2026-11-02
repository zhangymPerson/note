package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 寻址里的两段长度上限，与 store / web 同值：标题 1-200 码点、分类名 ≤30 码点。
const (
	maxRefTitleRunes    = 200
	maxRefCategoryRunes = 30
)

// QuickCategoryName 是 note set/get/del 不给分类时的默认分类（与 store.QuickCategoryName
// 同值，cmd/note 会断言两处一致）。cli 不 import store，名字只能各存一份。
const QuickCategoryName = "速记"

// Ref 是 <标题[@分类]> / @分类 的解析结果。
// Category 恒非空：没有显式分类时取调用方给的默认分类。
type Ref struct {
	Title        string
	Category     string
	CategoryOnly bool
}

// ParseRef 解析一份笔记引用。规则（顺序即优先级）：
//
//  1. typeFlag 非空 → 整体当标题，@ 不参与解析（标题里要写 @ 时的逃生口）；
//  2. raw 以 @ 开头 → 分类引用（只带分类，用于 get/del 整个分类）；
//  3. 否则按最后一个 @ 切成标题与分类；
//  4. 都没有 → 标题取自 raw，分类取 defaultCategory。
//
// 用法错误返回带指引的 error，由调用方映射成退出码 2。
func ParseRef(raw, typeFlag, defaultCategory string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	typeFlag = strings.TrimSpace(typeFlag)

	if raw == "" {
		return Ref{}, fmt.Errorf("缺少笔记引用：写成 <标题>、<标题>@<分类> 或 @<分类>（用法见 note set/get/del --help）")
	}

	ref := Ref{Category: defaultCategory}
	title := raw

	switch {
	case typeFlag != "":
		ref.Category = typeFlag
	case strings.HasPrefix(raw, "@"):
		ref.Category = strings.TrimPrefix(raw, "@")
		ref.CategoryOnly = true
		title = ""
	default:
		if index := strings.LastIndex(raw, "@"); index >= 0 {
			title = raw[:index]
			ref.Category = raw[index+1:]
		}
	}

	title = strings.TrimSpace(title)
	ref.Category = strings.TrimSpace(ref.Category)

	if ref.CategoryOnly {
		if title != "" {
			return Ref{}, fmt.Errorf("分类引用里不能再带标题（得到 %q）", raw)
		}
	} else {
		if title == "" {
			return Ref{}, fmt.Errorf("标题不能为空（得到 %q）", raw)
		}
		if utf8.RuneCountInString(title) > maxRefTitleRunes {
			return Ref{}, fmt.Errorf("标题最多 %d 个字符（得到 %d 个）", maxRefTitleRunes, utf8.RuneCountInString(title))
		}
	}

	if ref.Category == "" {
		return Ref{}, fmt.Errorf("分类名不能为空（得到 %q）", raw)
	}
	if utf8.RuneCountInString(ref.Category) > maxRefCategoryRunes {
		return Ref{}, fmt.Errorf("分类名最多 %d 个字符（得到 %d 个）", maxRefCategoryRunes, utf8.RuneCountInString(ref.Category))
	}

	ref.Title = title
	return ref, nil
}
