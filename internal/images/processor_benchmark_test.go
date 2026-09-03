// 本文件是业务回归测试，属于图片处理链路，负责封面、缩略图、阅读页图像的解码、缩放、缓存和 HTTP 条件请求支持。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func BenchmarkProcessImageResizeWebP(b *testing.B) {
	source := benchmarkPNG(b, 1200, 1800)
	opts := ProcessOptions{
		Width:   420,
		Format:  "webp",
		Quality: 82,
		Filter:  "lanczos3",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, contentType, err := ProcessImage(source, "image/png", opts)
		if err != nil {
			b.Fatalf("process image failed: %v", err)
		}
		if len(data) == 0 || contentType != "image/webp" {
			b.Fatalf("unexpected output: bytes=%d content_type=%s", len(data), contentType)
		}
	}
}

func BenchmarkProcessImageAutoCropPNG(b *testing.B) {
	source := benchmarkPNG(b, 900, 1300)
	opts := ProcessOptions{
		AutoCrop: true,
		Format:   "png",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, contentType, err := ProcessImage(source, "image/png", opts)
		if err != nil {
			b.Fatalf("process image failed: %v", err)
		}
		if len(data) == 0 || contentType != "image/png" {
			b.Fatalf("unexpected output: bytes=%d content_type=%s", len(data), contentType)
		}
	}
}

// BenchmarkProcessImageAutoCropAtOrigin 盯住裁切框左上角落在 (0,0) 的那条路径：
// 此时子图起点干净、Stride 却仍是父图的，归一化必须发生，而归一化就是一次整图拷贝。
func BenchmarkProcessImageAutoCropAtOrigin(b *testing.B) {
	source := cropAtOriginPNG(b, 900, 1300, 60)
	opts := ProcessOptions{
		AutoCrop: true,
		Format:   "png",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, contentType, err := ProcessImage(source, "image/png", opts)
		if err != nil {
			b.Fatalf("process image failed: %v", err)
		}
		if len(data) == 0 || contentType != "image/png" {
			b.Fatalf("unexpected output: bytes=%d content_type=%s", len(data), contentType)
		}
	}
}

// BenchmarkProcessImageAutoCropJPEG 是裁边路径上最贵的一档：JPEG 解出 *image.YCbCr，
// 归一化的画布类型选错就会掉进 image/draw 的逐像素通用路径，一页几百万次分配。
func BenchmarkProcessImageAutoCropJPEG(b *testing.B) {
	source := benchmarkJPEG(b, 900, 1300)
	opts := ProcessOptions{
		AutoCrop: true,
		Quality:  85,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, contentType, err := ProcessImage(source, "image/jpeg", opts)
		if err != nil {
			b.Fatalf("process image failed: %v", err)
		}
		if len(data) == 0 || contentType != "image/jpeg" {
			b.Fatalf("unexpected output: bytes=%d content_type=%s", len(data), contentType)
		}
	}
}

func benchmarkJPEG(b *testing.B, width, height int) []byte {
	b.Helper()

	decoded, _, err := image.Decode(bytes.NewReader(benchmarkPNG(b, width, height)))
	if err != nil {
		b.Fatalf("decode benchmark source failed: %v", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, decoded, &jpeg.Options{Quality: 90}); err != nil {
		b.Fatalf("encode source jpeg failed: %v", err)
	}
	return buf.Bytes()
}

func benchmarkPNG(b *testing.B, width, height int) []byte {
	b.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < 24 || y < 24 || x >= width-24 || y >= height-24 {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				continue
			}
			shade := uint8((x + y) % 210)
			img.SetRGBA(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		b.Fatalf("encode source png failed: %v", err)
	}
	return buf.Bytes()
}
