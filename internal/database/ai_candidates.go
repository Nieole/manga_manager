// 本文件为 AI 阅读推荐挑选候选系列：采样必须走主键范围探针而不是 ORDER BY RANDOM()（那会给
// 每一行算随机数再整表排序，代价随库规模线性增长）；「排除已读过半」必须按当前用户读取每用户
// 进度，不得依赖 books.last_read_page 那个已停写的全局列。

package database

import (
	"context"
	"database/sql"
	"math/rand/v2"
)

// CandidateSeriesForAI 是喂给 LLM 的候选系列。
type CandidateSeriesForAI struct {
	ID        int64
	Title     sql.NullString
	Name      string
	Summary   sql.NullString
	CoverPath sql.NullString
}

// candidateSeriesSelect 是候选行的取数部分，供分层探针与兜底扫描共用。
// %s 处填 id 范围条件。
const candidateSeriesSelect = `
SELECT s.id, s.title, s.name, s.summary,
       (SELECT b.cover_path FROM books b
         WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
         ORDER BY b.sort_number, b.name LIMIT 1) AS cover_path
FROM series s
WHERE s.summary IS NOT NULL AND s.summary != ''
  AND (
        s.total_pages = 0
        OR CAST(s.book_count AS REAL) <= 0
        OR (
          SELECT COUNT(*) FROM books b
           WHERE b.series_id = s.id
             AND (
                   -- userID > 0 走每用户进度；否则回落到全局列（未登录/单用户旧数据）。
                   CASE WHEN ?1 > 0
                        THEN EXISTS (SELECT 1 FROM user_book_progress ubp
                                      WHERE ubp.user_id = ?1 AND ubp.book_id = b.id AND ubp.last_read_page > 0)
                        ELSE b.last_read_page > 0
                   END
                 )
        ) < s.book_count * 0.5
      )
`

// SampleCandidateSeriesForAI 随机取至多 limit 个候选系列。
//
// 做法是「分层探针」：把 [minID, maxID] 按候选数切成 limit 个区间，每个区间取第一条命中行。
// 每次探针都是主键范围查找（SEARCH s USING INTEGER PRIMARY KEY），不必给全表算随机数、更不必排序。
//
// 分层探针天然产出 id 升序，直接喂给 LLM 会因为 prompt 的位置偏好而系统性偏向最早入库的作品，
// 所以返回前必须再洗一次牌。
func (s *SqlStore) SampleCandidateSeriesForAI(ctx context.Context, userID int64, limit int64) ([]CandidateSeriesForAI, error) {
	if limit <= 0 {
		return nil, nil
	}

	// 先用 MIN/MAX 把范围收窄到「确实有候选」的区间：库里 id 稀疏（删过库/系列）时，
	// 拿 1..maxID 平铺会让绝大多数区间落空，分层退化成全表遍历。
	var minID, maxID sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MIN(s.id), MAX(s.id) FROM series s WHERE s.summary IS NOT NULL AND s.summary != ''`,
	).Scan(&minID, &maxID); err != nil {
		return nil, err
	}
	if !minID.Valid || !maxID.Valid {
		return nil, nil // 全库没有带简介的系列
	}

	span := maxID.Int64 - minID.Int64 + 1
	width := span / limit
	if width < 1 {
		width = 1
	}

	seen := make(map[int64]struct{}, limit)
	out := make([]CandidateSeriesForAI, 0, limit)
	probe := func(query string, args ...interface{}) error {
		row := s.db.QueryRowContext(ctx, query, args...)
		var c CandidateSeriesForAI
		if err := row.Scan(&c.ID, &c.Title, &c.Name, &c.Summary, &c.CoverPath); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}
		if _, dup := seen[c.ID]; dup {
			return nil
		}
		seen[c.ID] = struct{}{}
		out = append(out, c)
		return nil
	}

	for i := int64(0); i < limit && int64(len(out)) < limit; i++ {
		lo := minID.Int64 + i*width
		hi := lo + width
		if i == limit-1 {
			hi = maxID.Int64 + 1 // 最后一个区间兜住余数
		}
		// 区间内随机起点，避免每次都返回区间里的第一条（否则同一份库每次采样结果完全相同）。
		start := lo
		if hi-lo > 1 {
			start = lo + rand.Int64N(hi-lo)
		}
		if err := probe(candidateSeriesSelect+` AND s.id >= ?2 AND s.id < ?3 ORDER BY s.id LIMIT 1`,
			userID, start, hi); err != nil {
			return nil, err
		}
	}

	// 兜底：随机起点会让区间尾部的候选被跳过，稀疏库上尤其明显。
	// 只在「分层确实取到了东西但没取够」时才跑——len(out)==0 意味着区间已精确平铺过全域
	// 且每段都被扫穿，此时全库零候选，再扫一遍必然也是空的，白付一次完整主键遍历。
	if len(out) > 0 && int64(len(out)) < limit {
		rows, err := s.db.QueryContext(ctx, candidateSeriesSelect+` ORDER BY s.id LIMIT ?2`, userID, limit*4)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() && int64(len(out)) < limit {
			var c CandidateSeriesForAI
			if err := rows.Scan(&c.ID, &c.Title, &c.Name, &c.Summary, &c.CoverPath); err != nil {
				return nil, err
			}
			if _, dup := seen[c.ID]; dup {
				continue
			}
			seen[c.ID] = struct{}{}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// 洗牌：见函数头注释——分层探针产出 id 升序，不洗会让推荐系统性偏向老作品。
	for i := len(out) - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
