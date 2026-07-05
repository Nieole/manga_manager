// 业务说明：本文件是业务回归测试，覆盖漫画卷/话号自然排序的边界与优先级规则。
// 它通过自动化断言保护 CJK 序数、小数章、缺号、10-vs-2、年份噪音等排序语义在重构后保持一致。

package booksort

import (
	"database/sql"
	"sort"
	"testing"

	"manga-manager/internal/database"
)

// TestExtractSortNumberMixedCJKForms 覆盖多种中日卷/话形式与小数章、缺号回退。
func TestExtractSortNumberMixedCJKForms(t *testing.T) {
	cases := []struct {
		label   string
		want    float64
		wantOK  bool
		comment string
	}{
		{"第5卷.cbz", 5, true, "卷 suffix"},
		{"5.5卷.cbz", 5.5, true, "decimal volume without 第 prefix"},
		{"第2.5话.cbz", 2.5, true, "decimal chapter"},
		{"第123集.cbz", 123, true, "集 suffix"},
		{"第一百二十三话.cbz", 123, true, "chinese hundreds"},
		{"第两千零一话.cbz", 2001, true, "chinese thousands with zero"},
		{"第一万话.cbz", 10000, true, "chinese ten-thousand"},
		// 混合前缀：CJK 序数优先于西文 Vol 前缀。
		{"Vol.5 第3话.cbz", 3, true, "cjk chapter beats latin vol prefix"},
		{"特别篇.cbz", 0, false, "no number at all"},
		{"", 0, false, "empty label"},
		{"   ", 0, false, "whitespace only"},
	}
	for _, tc := range cases {
		got, ok := ExtractSortNumber(tc.label)
		if ok != tc.wantOK || (tc.wantOK && got != tc.want) {
			t.Fatalf("ExtractSortNumber(%q) = %v,%v; want %v,%v (%s)", tc.label, got, ok, tc.want, tc.wantOK, tc.comment)
		}
	}
}

// TestExtractSortNumberTenVsTwoNaturalOrder 确认 10 排在 2 之后（非字典序）。
func TestExtractSortNumberTenVsTwoNaturalOrder(t *testing.T) {
	two, ok1 := ExtractSortNumber("第2话")
	ten, ok2 := ExtractSortNumber("第10话")
	if !ok1 || !ok2 {
		t.Fatalf("expected both to parse, got %v,%v", ok1, ok2)
	}
	if !(two < ten) {
		t.Fatalf("expected 2 < 10, got %v vs %v", two, ten)
	}
}

// TestEffectiveBookSortNumberFieldPriority 覆盖 Name > Title > Number(float) > Number(extract) > SortNumber 的取号优先级。
func TestEffectiveBookSortNumberFieldPriority(t *testing.T) {
	// Name 命中即使 SortNumber 存在旧值。
	b := database.Book{Name: "第3话.cbz", SortNumber: sql.NullFloat64{Float64: 99, Valid: true}}
	if v, ok := EffectiveBookSortNumber(b); !ok || v != 3 {
		t.Fatalf("Name should win: got %v,%v want 3,true", v, ok)
	}

	// Name 无号时用 Title。
	b = database.Book{Name: "SpecialEdition", Title: sql.NullString{String: "第7卷", Valid: true}}
	if v, ok := EffectiveBookSortNumber(b); !ok || v != 7 {
		t.Fatalf("Title fallback failed: got %v,%v want 7,true", v, ok)
	}

	// Number 为纯浮点串时用 ParseFloat。
	b = database.Book{Name: "noDigits", Number: sql.NullString{String: "12", Valid: true}}
	if v, ok := EffectiveBookSortNumber(b); !ok || v != 12 {
		t.Fatalf("Number float parse failed: got %v,%v want 12,true", v, ok)
	}

	// Number 为 CJK 序数串时用 ExtractSortNumber。
	b = database.Book{Name: "noDigits", Number: sql.NullString{String: "第8话", Valid: true}}
	if v, ok := EffectiveBookSortNumber(b); !ok || v != 8 {
		t.Fatalf("Number extract fallback failed: got %v,%v want 8,true", v, ok)
	}

	// 全部失败时回退 SortNumber。
	b = database.Book{Name: "noDigits", SortNumber: sql.NullFloat64{Float64: 5.5, Valid: true}}
	if v, ok := EffectiveBookSortNumber(b); !ok || v != 5.5 {
		t.Fatalf("SortNumber fallback failed: got %v,%v want 5.5,true", v, ok)
	}

	// 什么都没有 -> (0,false)。
	if v, ok := EffectiveBookSortNumber(database.Book{}); ok || v != 0 {
		t.Fatalf("empty book should yield 0,false; got %v,%v", v, ok)
	}
}

// TestCompareBooksVolumeDominatesNumber 确认卷号比较先于章号，即使章号相反。
func TestCompareBooksVolumeDominatesNumber(t *testing.T) {
	a := database.Book{Volume: "第2卷", Name: "第100话"}
	b := database.Book{Volume: "第1卷", Name: "第1话"}
	if CompareBooks(a, b) <= 0 {
		t.Fatalf("expected a(vol2) to sort after b(vol1) regardless of chapter, got %d", CompareBooks(a, b))
	}
	if CompareBooks(b, a) >= 0 {
		t.Fatalf("expected b(vol1) before a(vol2), got %d", CompareBooks(b, a))
	}
}

// TestCompareBooksNumberedBeforeUnnumbered 确认可解析号的书排在无号书之前。
func TestCompareBooksNumberedBeforeUnnumbered(t *testing.T) {
	numbered := database.Book{Name: "第1话.cbz"}
	unnumbered := database.Book{Name: "afterword.cbz"}
	if CompareBooks(numbered, unnumbered) >= 0 {
		t.Fatalf("expected numbered before unnumbered, got %d", CompareBooks(numbered, unnumbered))
	}
	if CompareBooks(unnumbered, numbered) <= 0 {
		t.Fatalf("expected unnumbered after numbered, got %d", CompareBooks(unnumbered, numbered))
	}
}

// TestCompareLabelsEqualFoldAndTieBreak 覆盖大小写等价返回 0，以及同号时的稳定字符串次序。
func TestCompareLabelsEqualFoldAndTieBreak(t *testing.T) {
	if CompareLabels("ABC", "abc") != 0 {
		t.Fatalf("case-insensitive equal labels should compare equal")
	}
	// 同号（都解析为 1）时按小写字典序决胜，保证稳定次序。
	if CompareLabels("Chapter 1 alpha", "Chapter 1 beta") >= 0 {
		t.Fatalf("expected alpha before beta for same-number labels")
	}
	if CompareLabels("Chapter 1 beta", "Chapter 1 alpha") <= 0 {
		t.Fatalf("expected beta after alpha for same-number labels")
	}
}

// TestCompareLabelsSortsLatinVolumesNaturally 确认 v1/v2/v10 西文前缀按数值排序。
func TestCompareLabelsSortsLatinVolumesNaturally(t *testing.T) {
	labels := []string{"v10", "v2", "v1"}
	sort.SliceStable(labels, func(i, j int) bool {
		return CompareLabels(labels[i], labels[j]) < 0
	})
	want := []string{"v1", "v2", "v10"}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("unexpected order: got %+v want %+v", labels, want)
		}
	}
}
