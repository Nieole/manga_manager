// 业务说明：本文件是业务回归测试，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package booksort

import (
	"database/sql"
	"sort"
	"testing"

	"manga-manager/internal/database"
)

func TestExtractSortNumberSupportsChineseOrdinalChapters(t *testing.T) {
	tests := []struct {
		name string
		want float64
	}{
		{"第一话.cbz", 1},
		{"第二話.cbz", 2},
		{"第十话.cbz", 10},
		{"第十一话.cbz", 11},
		{"第二十话.cbz", 20},
		{"第二十一话.cbz", 21},
		{"第一百零二话.cbz", 102},
		{"壹佰贰拾叁話.cbz", 123},
		{"100日后 第一话.cbz", 1},
		{"第2.5话.cbz", 2.5},
	}

	for _, tt := range tests {
		got, ok := ExtractSortNumber(tt.name)
		if !ok || got != tt.want {
			t.Fatalf("ExtractSortNumber(%q) = %v, %v; want %v, true", tt.name, got, ok, tt.want)
		}
	}
}

func TestCompareBooksUsesParsedChapterNumberBeforeLegacyZeroSortNumber(t *testing.T) {
	books := []database.Book{
		{ID: 10, Name: "第十话.cbz", SortNumber: sql.NullFloat64{Float64: 0, Valid: true}},
		{ID: 2, Name: "第二话.cbz", SortNumber: sql.NullFloat64{Float64: 0, Valid: true}},
		{ID: 11, Name: "第十一话.cbz", SortNumber: sql.NullFloat64{Float64: 0, Valid: true}},
		{ID: 1, Name: "第一话.cbz", SortNumber: sql.NullFloat64{Float64: 0, Valid: true}},
	}

	sort.SliceStable(books, func(i, j int) bool {
		return CompareBooks(books[i], books[j]) < 0
	})

	got := []int64{books[0].ID, books[1].ID, books[2].ID, books[3].ID}
	want := []int64{1, 2, 10, 11}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected order: got %+v want %+v", got, want)
		}
	}
}

func TestCompareLabelsSupportsChineseOrdinalVolumes(t *testing.T) {
	volumes := []string{"第十一卷", "第二卷", "第十卷", "第一卷"}
	sort.SliceStable(volumes, func(i, j int) bool {
		return CompareLabels(volumes[i], volumes[j]) < 0
	})

	want := []string{"第一卷", "第二卷", "第十卷", "第十一卷"}
	for i := range want {
		if volumes[i] != want[i] {
			t.Fatalf("unexpected volume order: got %+v want %+v", volumes, want)
		}
	}
}
