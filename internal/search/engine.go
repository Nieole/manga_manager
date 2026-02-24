package search

import (
	"fmt"
	"log"
	"manga-manager/internal/database"
	"os"
	"path/filepath"
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
		ID:         fmt.Sprintf("%d", book.ID),
		Type:       "book",
		Title:      title,
		SeriesName: seriesName,
	}

	return e.index.Index(doc.ID, doc)
}

func (e *Engine) Search(queryStr string, limit int) (*bleve.SearchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 使用支持模糊或包含切词的 Query String
	// 补充两侧星号支持任意位置的子串模糊匹配（这对于中文切词和连载名截断非常重要）
	qStr := "*" + queryStr + "*"
	query := bleve.NewQueryStringQuery(qStr)
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = limit
	// 要求返回哪些切片字段
	searchRequest.Fields = []string{"id", "title", "series_name"}

	return e.index.Search(searchRequest)
}
