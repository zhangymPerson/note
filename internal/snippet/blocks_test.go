package snippet

import (
	"reflect"
	"testing"
)

func TestFenceBlocks(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []Block
	}{
		{
			name:  "无围栏",
			lines: []string{"a", "b", "c"},
			want:  nil,
		},
		{
			name:  "完整代码块含语言标注",
			lines: []string{"前", "```go", "code", "```", "后"},
			want:  []Block{{Start: 1, End: 3}},
		},
		{
			name:  "未闭合围栏延伸到末尾",
			lines: []string{"```", "code", "more"},
			want:  []Block{{Start: 0, End: 2}},
		},
		{
			name:  "~~~ 与 ``` 互不打断",
			lines: []string{"```", "~~~", "```"},
			want:  []Block{{Start: 0, End: 2}},
		},
		{
			name:  "连续两个代码块",
			lines: []string{"```", "a", "```", "```", "b", "```"},
			want:  []Block{{Start: 0, End: 2}, {Start: 3, End: 5}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FenceBlocks(tc.lines); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("FenceBlocks = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTableBlocks(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []Block
	}{
		{
			name:  "无表格",
			lines: []string{"普通行", "还是普通"},
			want:  nil,
		},
		{
			name:  "完整表格（表头+分隔+两行）",
			lines: []string{"说明", "| 选项 | 说明 |", "| --- | --- |", "| A | 1 |", "结尾"},
			want:  []Block{{Start: 1, End: 3}},
		},
		{
			name:  "相邻表格被空行隔开",
			lines: []string{"| a |", "", "| b |"},
			want:  []Block{{Start: 0, End: 0}, {Start: 2, End: 2}},
		},
		{
			name:  "单管道不是表格行",
			lines: []string{"| a |", "|", "普通"},
			want:  []Block{{Start: 0, End: 0}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TableBlocks(tc.lines); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("TableBlocks = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestContaining(t *testing.T) {
	blocks := []Block{{Start: 1, End: 3}}
	if b := Containing(2, blocks); b == nil || *b != blocks[0] {
		t.Fatalf("Containing(2) = %+v, want %+v", b, blocks[0])
	}
	if b := Containing(4, blocks); b != nil {
		t.Fatalf("Containing(4) 应返回 nil, got %+v", b)
	}
}
