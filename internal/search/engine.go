package search

import (
	"fmt"
	"log"
	"manga-manager/internal/database"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
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
		log.Println("Created new Bleve search index.")
	} else {
		idx, err = bleve.Open(indexPath)
		if err != nil {
			return nil, err
		}
		log.Println("Opened existing Bleve search index.")
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

	return e.index.Index(doc.ID, doc)
}

func (e *Engine) IndexSeries(id int64, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	doc := BookDocument{
		ID:         fmt.Sprintf("s_%d", id),
		Type:       "series",
		Title:      name,
		SeriesName: name,
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

	var searchRequest *bleve.SearchRequest

	if target == "series" {
		qPhrase.SetField("series_name")
		qWild.SetField("series_name")
		baseQuery := bleve.NewDisjunctionQuery(qPhrase, qWild)

		typeQuery := bleve.NewMatchQuery("series")
		typeQuery.SetField("type")
		searchRequest = bleve.NewSearchRequest(bleve.NewConjunctionQuery(baseQuery, typeQuery))
	} else if target == "book" || target == "title" {
		qPhrase.SetField("title")
		qWild.SetField("title")
		baseQuery := bleve.NewDisjunctionQuery(qPhrase, qWild)

		typeQuery := bleve.NewMatchQuery("book")
		typeQuery.SetField("type")
		searchRequest = bleve.NewSearchRequest(bleve.NewConjunctionQuery(baseQuery, typeQuery))
	} else {
		baseQuery := bleve.NewDisjunctionQuery(qPhrase, qWild)
		searchRequest = bleve.NewSearchRequest(baseQuery)
	}

	searchRequest.Size = limit
	// 要求返回哪些切片字段
	searchRequest.Fields = []string{"id", "title", "series_name", "type"}

	return e.index.Search(searchRequest)
}
