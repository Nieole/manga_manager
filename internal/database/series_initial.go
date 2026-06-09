// 业务说明：本文件是业务实现，属于 SQLite 数据访问层，负责把漫画库、系列、阅读进度、任务和元数据状态持久化为稳定数据模型。
// 它连接 sqlc 生成查询与上层领域服务，是资料库筛选、搜索同步和关系图谱的数据基础。
// 维护时应保持 schema、查询定义、事务边界和迁移兼容，避免破坏既有用户数据。

package database

import (
	"database/sql"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

var pinyinArgs = pinyin.NewArgs()

func init() {
	pinyinArgs.Style = pinyin.Normal
	pinyinArgs.Heteronym = false
}

// SeriesInitial returns the A-Z shelf initial for a series display name.
func SeriesInitial(title, name string) string {
	displayName := strings.TrimSpace(title)
	if displayName == "" {
		displayName = name
	}

	for _, r := range displayName {
		if r >= 'a' && r <= 'z' {
			return string(r - 'a' + 'A')
		}
		if r >= 'A' && r <= 'Z' {
			return string(r)
		}
		if unicode.Is(unicode.Han, r) {
			py := pinyin.SinglePinyin(r, pinyinArgs)
			if len(py) == 0 || py[0] == "" {
				return "#"
			}
			initial := py[0][0]
			if initial >= 'a' && initial <= 'z' {
				return string(initial - 'a' + 'A')
			}
			if initial >= 'A' && initial <= 'Z' {
				return string(initial)
			}
			return "#"
		}
	}

	return "#"
}

func SeriesInitialFromNullTitle(title sql.NullString, name string) string {
	if !title.Valid {
		return SeriesInitial("", name)
	}
	return SeriesInitial(title.String, name)
}
