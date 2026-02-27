package search

import (
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

type Engine struct {
	index bleve.Index
	mu    sync.RWMutex
}

func NewEngine(dataPath string) (*Engine, error) {
	indexPath := filepath.Join(dataPath, "search.bleve")
	var idx bleve.Index
	var err error

	if _, errStat := os.Stat(indexPath); os.IsNotExist(errStat) {
		// 使用默认的中文字段分词器支持映射
		mapping := bleve.NewIndexMapping()
		idx, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, err
		}
		slog.Info("Created new Bleve search index.")
	} else {
		idx, err = bleve.Open(indexPath)
		if err != nil {
			return nil, err
		}
		slog.Info("Opened existing Bleve search index.")
	}

	return &Engine{
		index: idx,
	}, nil
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.index != nil {
		return e.index.Close()
	}
	return nil
}

type BookDocument struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	SeriesName string `json:"series_name"`
	CoverPath  string `json:"cover_path"`
}

// IndexBook 将书籍及其系列名推入分词引擎打标
func (e *Engine) IndexBook(book database.Book, seriesName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	title := book.Name
	if book.Title.Valid && book.Title.String != "" {
		title = book.Title.String
	}

	doc := BookDocument{
		ID:         fmt.Sprintf("b_%d", book.ID),
		Type:       "book",
		Title:      title,
		SeriesName: seriesName,
	}
	if book.CoverPath.Valid {
		doc.CoverPath = book.CoverPath.String
	}

	return e.index.Index(doc.ID, doc)
}

func (e *Engine) IndexSeries(id int64, name string, coverPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	doc := BookDocument{
		ID:         fmt.Sprintf("s_%d", id),
		Type:       "series",
		Title:      name,
		SeriesName: name,
		CoverPath:  coverPath,
	}

	return e.index.Index(doc.ID, doc)
}

func (e *Engine) Search(queryStr string, target string, limit int) (*bleve.SearchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 使用带分词的短语匹配，要求词语顺序与字面一致，防止"N和S"被过度容错拆解
	qPhrase := bleve.NewMatchPhraseQuery(queryStr)

	// 使用通配符匹配，解决未输入完整的半截残词（例如 "30" 匹配 "303"）
	cleanQuery := strings.ToLower(strings.TrimSpace(queryStr))
	qWild := bleve.NewWildcardQuery("*" + cleanQuery + "*")

	// 针对诸如 "NS" -> "N和S" 的缩写/跳字查询
	var qAcronym *query.ConjunctionQuery
	runes := []rune(cleanQuery)
	if len(runes) > 1 && len(runes) <= 6 && !strings.Contains(cleanQuery, " ") {
		var charQueries []query.Query
		for _, ch := range runes {
			charQueries = append(charQueries, bleve.NewMatchQuery(string(ch)))
		}
		qAcronym = bleve.NewConjunctionQuery(charQueries...)
	}

	var searchRequest *bleve.SearchRequest

	buildBaseQuery := func(field string) query.Query {
		qPhrase.SetField(field)
		qWild.SetField(field)
		if qAcronym != nil {
			for _, child := range qAcronym.Conjuncts {
				if m, ok := child.(*query.MatchQuery); ok {
					m.SetField(field)
				}
			}
			return bleve.NewDisjunctionQuery(qPhrase, qWild, qAcronym)
		}
		return bleve.NewDisjunctionQuery(qPhrase, qWild)
	}

	if target == "series" {
		baseQuery := buildBaseQuery("series_name")
		typeQuery := bleve.NewMatchQuery("series")
		typeQuery.SetField("type")
		searchRequest = bleve.NewSearchRequest(bleve.NewConjunctionQuery(baseQuery, typeQuery))
	} else if target == "book" || target == "title" {
		baseQuery := buildBaseQuery("title")
		typeQuery := bleve.NewMatchQuery("book")
		typeQuery.SetField("type")
		searchRequest = bleve.NewSearchRequest(bleve.NewConjunctionQuery(baseQuery, typeQuery))
	} else {
		var baseQuery query.Query
		if qAcronym != nil {
			var qs []query.Query
			qs = append(qs, bleve.NewMatchPhraseQuery(queryStr))
			qs = append(qs, bleve.NewWildcardQuery("*"+cleanQuery+"*"))
			for _, child := range qAcronym.Conjuncts {
				qs = append(qs, child) // wait, qAcronym is already a query so we append it directly
			}
			// Let's just create a new one safely
			qGlobalAcronym := bleve.NewConjunctionQuery()
			for _, child := range qAcronym.Conjuncts {
				if m, ok := child.(*query.MatchQuery); ok {
					qGlobalAcronym.AddQuery(bleve.NewMatchQuery(m.Match))
				}
			}
			baseQuery = bleve.NewDisjunctionQuery(bleve.NewMatchPhraseQuery(queryStr), bleve.NewWildcardQuery("*"+cleanQuery+"*"), qGlobalAcronym)
		} else {
			baseQuery = bleve.NewDisjunctionQuery(bleve.NewMatchPhraseQuery(queryStr), bleve.NewWildcardQuery("*"+cleanQuery+"*"))
		}
		searchRequest = bleve.NewSearchRequest(baseQuery)
	}

	searchRequest.Size = limit
	// 要求返回哪些切片字段
	searchRequest.Fields = []string{"id", "title", "series_name", "type", "cover_path"}

	return e.index.Search(searchRequest)
}
