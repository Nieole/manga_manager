package images

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/chai2010/webp"
	"github.com/nfnt/resize"
	golangWebp "golang.org/x/image/webp"

	// Init defaults for read fallback
	_ "image/gif"

	_ "github.com/gen2brain/avif"
)

// ProcessOptions 用于接受前端动态要求的尺寸转换
type ProcessOptions struct {
	Width   int
	Height  int
	Format  string // webp, jpeg, png
	Quality int    // 0-100
}

func ProcessImage(data []byte, contentType string, opts ProcessOptions) ([]byte, string, error) {
	if opts.Width == 0 && opts.Height == 0 {
		return data, contentType, nil
	}

	img, _, err := decodeImage(data, contentType)
	if err != nil {
		return nil, "", fmt.Errorf("decode image err: %w", err)
	}

	var newImg image.Image = img
	if opts.Width > 0 || opts.Height > 0 {
		newImg = resize.Resize(uint(opts.Width), uint(opts.Height), img, resize.Bilinear)
	}

	var buf bytes.Buffer
	var newContentType string

	format := strings.ToLower(opts.Format)
	if format == "" {
		format = "jpeg" // 默认退化到高压缩的 jpeg 给缩略图
	}

	switch format {
	case "png":
		err = png.Encode(&buf, newImg)
		newContentType = "image/png"
	case "webp":
		opt := &webp.Options{Lossless: false, Quality: float32(opts.Quality)}
		if opt.Quality <= 0 {
			opt.Quality = 85 // 默认质量
		}
		err = webp.Encode(&buf, newImg, opt)
		newContentType = "image/webp"
	default:
		// Fallback everything else to JPEG to save space
		opt := &jpeg.Options{Quality: opts.Quality}
		if opt.Quality <= 0 {
			opt.Quality = 85 // 默认质量对于缩略图足够
		}
		err = jpeg.Encode(&buf, newImg, opt)
		newContentType = "image/jpeg"
	}

	if err != nil {
		return nil, "", fmt.Errorf("encode image err: %w", err)
	}

	return buf.Bytes(), newContentType, nil
}

func decodeImage(data []byte, contentType string) (image.Image, string, error) {
	reader := bytes.NewReader(data)
	if strings.Contains(contentType, "webp") {
		img, err := golangWebp.Decode(reader)
		return img, "webp", err
	}

	return image.Decode(reader)
}
