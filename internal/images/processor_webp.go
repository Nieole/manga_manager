// 业务说明：本文件是业务实现，属于图片处理链路，负责封面、缩略图、阅读页图像的解码、缩放、缓存和 HTTP 条件请求支持。
// 它直接影响资料库列表、系列详情、阅读器和关系图谱中的图片加载速度与流量占用。
// 维护时应重点关注缓存键、ETag/Last-Modified、格式兼容、并发读写和大图内存占用。

package images

import (
	"image"
	"io"

	"github.com/chai2010/webp"
)

func encodeWebP(w io.Writer, img image.Image, quality int, lossless bool) (string, error) {
	err := webp.Encode(w, img, &webp.Options{
		Lossless: lossless,
		Quality:  float32(quality),
	})
	return "image/webp", err
}

func webpIntermediateExtension() string {
	return "webp"
}
