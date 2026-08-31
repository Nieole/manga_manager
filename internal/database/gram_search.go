// 本文件把「短关键字搜索」接到 2-gram 辅助索引上。
//
// FTS5 的 trigram 分词器对 <3 字符的关键字不产生任何 token，于是中文用户搜两个字会回落到
// 全表 instr 扫描——200k 书实测每次 204ms，而全局搜索是 books + series 两轮。
// series_gram_fts / book_gram_fts 用「每个字符位置起一个 2 字窗口、编码成 UTF-8 字节的十六进制」
// 索引同一批文本（建表与触发器见 schema.sql / migrate.go），这里负责查询侧的对称编码。

package database

import (
	"encoding/hex"
	"strings"
)

// gramSearchMaxRunes 是走 gram 索引的关键字长度上限。
// >= 3 rune 由 trigram FTS 处理（seriesSearchFTSEligible），两者互补、不重叠。
const gramSearchMaxRunes = 2

// gramMatchExpr 把关键字编码成 FTS5 的 MATCH 表达式。
//
// 编码必须与索引侧**逐字节**对称，所以这里刻意不用 strings.ToLower：
// 索引是由 SQL 触发器里的 lower() 生成的，而 SQLite 无 ICU 时 lower() 只折叠 ASCII A-Z；
// Go 的 strings.ToLower 按 Unicode 折叠（土耳其语 İ、希腊语 Σ 等），两边规则一旦不同就永远对不上，
// 而且症状是「搜索静默返回空结果」，没有任何报错。
//
// 关键字必须先 TrimSpace：调用方传进来的是原始查询串（handler 里就是 query.Get("q")），
// 带首尾空格时字符数会多算，编出来的 token 与任何 2-gram 都不相等 —— 同样是静默的空结果。
func gramMatchExpr(keyword string) (string, bool) {
	trimmed := strings.TrimSpace(keyword)
	runes := []rune(trimmed)
	if len(runes) == 0 || len(runes) > gramSearchMaxRunes {
		return "", false
	}
	token := strings.ToUpper(hex.EncodeToString([]byte(asciiLower(trimmed))))
	if len(runes) == 1 {
		// 单字：索引里每个位置都起了一个 gram，所以该字必然是某个 token 的前缀。
		// UTF-8 自同步且多字节字符首字节 >= 0xC2，前缀匹配不会跨字符边界产生歧义。
		return `"` + token + `"*`, true
	}
	return `"` + token + `"`, true
}

// asciiLower 只折叠 ASCII 的 A-Z，与 SQLite 无 ICU 时的 lower() 行为一致。
func asciiLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// seriesGramFilter 是「系列命中短关键字」的 WHERE 片段，绑定一个参数（MATCH 表达式）。
const seriesGramFilter = `s.id IN (SELECT rowid FROM series_gram_fts WHERE series_gram_fts MATCH ?)`

// bookGramFilter 是「书命中短关键字，或其所属系列命中」的 WHERE 片段，绑定两个参数。
//
// 写成**单个 IN + 子查询内 UNION**，而不是 `b.id IN (...) OR s.id IN (...)`：
// 跨表的 OR（b.id 与 s.id 分属两张表）用不了 SQLite 的 OR-by-index 优化，外层仍要把
// 整张 books 走一遍；在非选择性关键字（中文书名里极常见的「第X」「X卷」）下这个代价
// 尤其明显。UNION 形态能让外层退化成主键点查（SEARCH b USING INTEGER PRIMARY KEY），
// 选择性与非选择性关键字都不会退化。
//
// 这个形态与既有的 searchGlobalBooksFTS 结构完全同构，一致性也更好。
const bookGramFilter = `b.id IN (
	SELECT rowid FROM book_gram_fts WHERE book_gram_fts MATCH ?
	UNION
	SELECT b2.id FROM series_gram_fts sg JOIN books b2 ON b2.series_id = sg.rowid
	 WHERE series_gram_fts MATCH ?
)`
