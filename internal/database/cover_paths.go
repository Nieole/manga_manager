// 本文件提供「被引用的封面路径」的流式遍历，服务于缩略图清理。
// 不要改回一次性返回切片：调用方（Scanner.CleanupThumbnails）只做流式去重，
// 大库下物化整表结果是纯浪费。

package database

import (
	"context"
	"path/filepath"
)

// referencedCoverPathsQuery 一次取回 books 与 series_stats 两侧的封面路径。
//
// 刻意用 UNION ALL 而不是 UNION/DISTINCT：后两者都要建 temp B-tree 物化去重，
// 而调用方拿去填 map，重复本来就无害。EXPLAIN QUERY PLAN 里不应出现 TEMP B-TREE。
const referencedCoverPathsQuery = `
SELECT cover_path FROM books        WHERE cover_path IS NOT NULL AND cover_path <> ''
UNION ALL
SELECT cover_path FROM series_stats WHERE cover_path IS NOT NULL AND cover_path <> ''`

// ForEachReferencedCoverPath 对每个被引用的封面路径调用一次 fn（已归一化为斜杠分隔）。
// 路径可能重复，调用方自行去重。
//
// 契约：fn 在数据库游标**打开期间**被调用，因此这段时间内连接被占用、WAL 读快照被持有。
// fn 里不要做长时间阻塞的事（尤其不要 taskcontrol.Wait 等待暂停闸），否则会把一个数据库
// 连接与一个读快照一起挂住。fn 返回错误即中止遍历并原样上抛。
func (s *SqlStore) ForEachReferencedCoverPath(ctx context.Context, fn func(path string) error) error {
	rows, err := s.db.QueryContext(ctx, referencedCoverPathsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		if path == "" {
			continue
		}
		if err := fn(filepath.ToSlash(path)); err != nil {
			return err
		}
	}
	return rows.Err()
}
