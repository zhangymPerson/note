// Package snippet 是搜索命中片段的行级块判定：fenced code 块与表格块的行区间
// 识别。internal/store 的 matchLines（网页搜索结果）与 internal/cli 的命中块渲染
// 共用这套规则，保证「命中处是否落在块里」两端同口径，不会各自演化漂移。
package snippet

import "strings"

// Block 是正文行级块结构（fenced code 或表格）的 [Start, End] 行区间，含两端。
type Block struct {
	Start int
	End   int
}

// FenceBlocks 返回 fenced code 块（``` 或 ~~~ 围栏）的区间。判定只看整行 trim 后
// 以围栏开头（可带语言标注，如 ```go）；未闭合的围栏一直延伸到内容末尾。
func FenceBlocks(lines []string) []Block {
	var (
		blocks []Block
		open   = -1
		marker string
	)
	for i, line := range lines {
		mark, isFence := fenceMarker(strings.TrimSpace(line))
		if open < 0 {
			if isFence {
				open, marker = i, mark
			}
			continue
		}
		// 在块内：遇到同一种围栏就收口（不同围栏不打断，如 ``` 里的 ~~~）
		if isFence && mark == marker {
			blocks = append(blocks, Block{Start: open, End: i})
			open = -1
		}
	}
	if open >= 0 {
		blocks = append(blocks, Block{Start: open, End: len(lines) - 1})
	}
	return blocks
}

// TableBlocks 返回连续表格行构成的块区间。表格行判定：trim 后以 | 开头且整行
// 至少两个 |；相邻表格行合成一块（含表头与分隔行），块之间被非表格行隔开。
func TableBlocks(lines []string) []Block {
	var blocks []Block
	start := -1
	for i, line := range lines {
		if isTableRow(line) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			blocks = append(blocks, Block{Start: start, End: i - 1})
			start = -1
		}
	}
	if start >= 0 {
		blocks = append(blocks, Block{Start: start, End: len(lines) - 1})
	}
	return blocks
}

// Containing 返回 blocks 中包含第 i 行的块；没有则返回 nil。
func Containing(i int, blocks []Block) *Block {
	for _, b := range blocks {
		if i >= b.Start && i <= b.End {
			return &b
		}
	}
	return nil
}

// fenceMarker 判断 trim 后的行是不是 fenced code 围栏行，返回围栏记号（``` 或 ~~~）。
func fenceMarker(t string) (string, bool) {
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, m) {
			return m, true
		}
	}
	return "", false
}

// isTableRow 判断一行是不是表格行：trim 后以 | 开头，且整行至少两个 |。
func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && strings.Count(t, "|") >= 2
}
