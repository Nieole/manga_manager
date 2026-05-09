package images

import (
	"bytes"
	"image"
	"image/color"
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
