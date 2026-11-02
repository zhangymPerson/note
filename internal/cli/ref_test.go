package cli

import (
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	const def = QuickCategoryName

	cases := []struct {
		name     string
		raw      string
		typeFlag string
		want     Ref
		wantErr  bool
	}{
		{name: "title only", raw: "周报", want: Ref{Title: "周报", Category: def}},
		{name: "title and category", raw: "周报@工作", want: Ref{Title: "周报", Category: "工作"}},
		{name: "category only", raw: "@工作", want: Ref{Category: "工作", CategoryOnly: true}},
		{name: "type flag keeps at sign in title", raw: "meeting@2024", typeFlag: "工作", want: Ref{Title: "meeting@2024", Category: "工作"}},
		{name: "splits on last at sign", raw: "a@b@c", want: Ref{Title: "a@b", Category: "c"}},
		{name: "empty category after at", raw: "周报@", wantErr: true},
		{name: "bare at sign", raw: "@", wantErr: true},
		{name: "empty ref", raw: "", wantErr: true},
		{name: "title too long", raw: strings.Repeat("字", maxRefTitleRunes+1), wantErr: true},
		{name: "category too long", raw: "周报@" + strings.Repeat("类", maxRefCategoryRunes+1), wantErr: true},
		{name: "type flag too long", raw: "周报", typeFlag: strings.Repeat("类", maxRefCategoryRunes+1), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRef(tc.raw, tc.typeFlag, def)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseRef(%q, %q) = %+v, want %+v", tc.raw, tc.typeFlag, got, tc.want)
			}
		})
	}
}

func TestParseRefTrimsWhitespace(t *testing.T) {
	got, err := ParseRef(" 周报 @ 工作 ", "", QuickCategoryName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Ref{Title: "周报", Category: "工作"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseRefKeepsQuickCategoryName(t *testing.T) {
	if QuickCategoryName != "速记" {
		t.Fatalf("default quick category name changed: %q", QuickCategoryName)
	}
}
