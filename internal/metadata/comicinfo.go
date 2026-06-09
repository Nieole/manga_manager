// 业务说明：本文件是业务实现，属于元数据聚合链路，负责从本地规则、外部站点和 AI Provider 获取漫画标题、简介、人物、标签与关系信息。
// 它支撑系列详情、智能补全、关系图谱和搜索索引的内容质量。
// 维护时应关注 Provider 契约、失败回退、限流、提示词稳定性和人工审核数据不被覆盖。

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
