package metadata

import (
	"context"
	"encoding/xml"
	"log/slog"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

// ComicInfo XML规范的一小部分参考定义 (https://github.com/anansi-project/comicinfo)
type ComicInfo struct {
	XMLName   xml.Name `xml:"ComicInfo"`
	Title     string   `xml:"Title"`
	Series    string   `xml:"Series"`
	Number    string   `xml:"Number"`
	Count     int      `xml:"Count"`
	Volume    int      `xml:"Volume"`
	Summary   string   `xml:"Summary"`
	Notes     string   `xml:"Notes"`
	Year      int      `xml:"Year"`
	Month     int      `xml:"Month"`
	Writer    string   `xml:"Writer"`
	Publisher string   `xml:"Publisher"`
	Genre     string   `xml:"Genre"`
	PageCount int      `xml:"PageCount"`
}

func ExtractAndApply(ctx context.Context, store database.Store, arc parser.Archive, bookID string, seriesID string) error {
	data, err := arc.ReadMetadataFile("ComicInfo.xml")
	if err != nil {
		// Log but return nil since it's common for books to miss this
		return nil
	}

	var info ComicInfo
	if err := xml.Unmarshal(data, &info); err != nil {
		slog.Warn("Failed to unmarshal ComicInfo.xml", "book_id", bookID, "error", err)
		return err
	}

	slog.Info("Found ComicInfo", "series", info.Series, "title", info.Title)

	// TODO: 实现更细粒度的 update store 更新以覆盖从文件结构中推导出的 title/summary 默认值。
	return nil
}
