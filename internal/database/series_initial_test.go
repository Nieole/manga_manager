// 业务说明：本文件是业务回归测试，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package database

import "testing"

func TestSeriesInitial(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		fallback string
		want     string
	}{
		{name: "chinese title", title: "进击的巨人", fallback: "folder", want: "J"},
		{name: "symbol prefixed chinese", title: "《火影忍者》", fallback: "folder", want: "H"},
		{name: "symbol prefixed english", title: "— One Piece", fallback: "folder", want: "O"},
		{name: "number prefixed chinese", title: "123-鬼灭之刃", fallback: "folder", want: "G"},
		{name: "fallback name", title: "", fallback: "【A版】Series", want: "A"},
		{name: "no letter", title: "12345...", fallback: "folder", want: "#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeriesInitial(tt.title, tt.fallback); got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}
