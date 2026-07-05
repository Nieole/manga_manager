// 业务说明：本文件是业务回归测试，属于图片处理链路，验证 ProcessImage 的透传短路、缩放尺寸、格式继承/转换、
// 解码炸弹拦截、非图片报错，以及自动裁白边、背景色判定与坐标归一化等纯逻辑，保障封面/缩略图/阅读页图像加工正确。
package images

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

// makeTestJPEG 生成一张纯色 JPEG，用于验证格式继承路径。
func makeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func decodeConfigDims(t *testing.T, data []byte) (int, int, string) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode output config: %v", err)
	}
	return cfg.Width, cfg.Height, format
}

// ---- ProcessImage 透传短路 ----

func TestProcessImagePassthroughWhenNoOps(t *testing.T) {
	src := makeTestPNG(t, 16, 16)
	out, ct, err := ProcessImage(src, "image/png", ProcessOptions{})
	if err != nil {
		t.Fatalf("ProcessImage passthrough failed: %v", err)
	}
	if ct != "image/png" {
		t.Fatalf("expected content type unchanged, got %s", ct)
	}
	// 透传应原样返回输入字节（不解码重编码）。
	if !bytes.Equal(out, src) {
		t.Fatalf("expected raw passthrough bytes, got re-encoded output (len %d vs %d)", len(out), len(src))
	}
}

func TestProcessImagePassthroughWhenFormatMatches(t *testing.T) {
	src := makeTestPNG(t, 16, 16)
	// format=png 与源 image/png 一致，且无其它加工 → 透传。
	out, ct, err := ProcessImage(src, "image/png", ProcessOptions{Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage failed: %v", err)
	}
	if ct != "image/png" || !bytes.Equal(out, src) {
		t.Fatalf("expected passthrough for matching format, got ct=%s equal=%v", ct, bytes.Equal(out, src))
	}
}

func TestProcessImageReencodesWhenFormatDiffers(t *testing.T) {
	src := makeTestPNG(t, 16, 16)
	// format=jpeg 与源 png 不一致 → 必须解码重编码，不能透传。
	out, ct, err := ProcessImage(src, "image/png", ProcessOptions{Format: "jpeg"})
	if err != nil {
		t.Fatalf("ProcessImage failed: %v", err)
	}
	if ct != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %s", ct)
	}
	if bytes.Equal(out, src) {
		t.Fatal("expected re-encoded bytes, got raw passthrough")
	}
	if _, _, format := decodeConfigDims(t, out); format != "jpeg" {
		t.Fatalf("expected jpeg output, decoded as %s", format)
	}
}

// ---- 缩放尺寸 ----

func TestProcessImageResizeExactDimensions(t *testing.T) {
	src := makeTestPNG(t, 64, 64)
	out, ct, err := ProcessImage(src, "image/png", ProcessOptions{Width: 32, Height: 48, Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage failed: %v", err)
	}
	if ct != "image/png" {
		t.Fatalf("expected image/png, got %s", ct)
	}
	w, h, _ := decodeConfigDims(t, out)
	if w != 32 || h != 48 {
		t.Fatalf("expected 32x48 output, got %dx%d", w, h)
	}
}

func TestProcessImageResizeWidthOnlyPreservesAspect(t *testing.T) {
	src := makeTestPNG(t, 64, 64)
	// 只给宽度，高度=0 → 保持纵横比缩放 → 32x32。
	out, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: 32, Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage failed: %v", err)
	}
	w, h, _ := decodeConfigDims(t, out)
	if w != 32 || h != 32 {
		t.Fatalf("expected aspect-preserving 32x32, got %dx%d", w, h)
	}
}

// ---- 格式继承 ----

func TestProcessImageFormatInheritance(t *testing.T) {
	// 未显式指定 Format 时应从源 contentType 继承格式（缩放触发重编码）。
	cases := []struct {
		name      string
		src       []byte
		srcCT     string
		wantCT    string
		wantImgFm string
	}{
		{"png source", makeTestPNG(t, 40, 40), "image/png", "image/png", "png"},
		{"jpeg source", makeTestJPEG(t, 40, 40), "image/jpeg", "image/jpeg", "jpeg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ct, err := ProcessImage(tc.src, tc.srcCT, ProcessOptions{Width: 20, Height: 20})
			if err != nil {
				t.Fatalf("ProcessImage failed: %v", err)
			}
			if ct != tc.wantCT {
				t.Fatalf("expected inherited content type %s, got %s", tc.wantCT, ct)
			}
			if _, _, format := decodeConfigDims(t, out); format != tc.wantImgFm {
				t.Fatalf("expected inherited image format %s, decoded %s", tc.wantImgFm, format)
			}
		})
	}
}

func TestProcessImageEncodesWebP(t *testing.T) {
	src := makeTestPNG(t, 24, 24)
	out, ct, err := ProcessImage(src, "image/png", ProcessOptions{Width: 12, Height: 12, Format: "webp", Quality: 80})
	if err != nil {
		t.Fatalf("ProcessImage webp failed: %v", err)
	}
	if ct != "image/webp" || len(out) == 0 {
		t.Fatalf("expected non-empty image/webp, got ct=%s len=%d", ct, len(out))
	}
}

// ---- 错误路径 ----

func TestProcessImageErrorOnNonImage(t *testing.T) {
	_, _, err := ProcessImage([]byte("definitely not an image payload"), "text/plain", ProcessOptions{Width: 10, Height: 10})
	if err == nil {
		t.Fatal("expected decode error for non-image input")
	}
	if !strings.Contains(err.Error(), "decode image err") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

// buildHugePNGHeader 构造一个仅含合法 IHDR（声明 20000x20000）的 PNG 头，
// 用于让 DecodeConfig 报告超大画布从而触发解码炸弹保护，无需真实分配像素。
func buildHugePNGHeader(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10}) // PNG 签名
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type: RGBA
	ihdr[10] = 0
	ihdr[11] = 0
	ihdr[12] = 0
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(ihdr)))
	buf.Write(lenBuf[:])
	chunk := append([]byte("IHDR"), ihdr...)
	buf.Write(chunk)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(chunk))
	buf.Write(crcBuf[:])
	return buf.Bytes()
}

func TestProcessImageRejectsDecodeBomb(t *testing.T) {
	bomb := buildHugePNGHeader(20000, 20000) // 4e8 像素 > maxDecodePixels(1e8)
	// 先确认 DecodeConfig 能读出声明的巨大尺寸（否则测试无法覆盖目标分支）。
	cfg, _, err := image.DecodeConfig(bytes.NewReader(bomb))
	if err != nil {
		t.Fatalf("crafted PNG header not parseable by DecodeConfig: %v", err)
	}
	if cfg.Width != 20000 || cfg.Height != 20000 {
		t.Fatalf("expected 20000x20000 header, got %dx%d", cfg.Width, cfg.Height)
	}
	_, _, perr := ProcessImage(bomb, "image/png", ProcessOptions{Width: 100, Height: 100})
	if perr == nil {
		t.Fatal("expected decode-bomb rejection")
	}
	if !strings.Contains(perr.Error(), "too large") {
		t.Fatalf("expected 'too large' error, got %v", perr)
	}
}

// ---- 纯逻辑：formatMatchesContentType ----

func TestFormatMatchesContentType(t *testing.T) {
	cases := []struct {
		format, ct string
		want       bool
	}{
		{"jpg", "image/jpeg", true}, // jpg 归一化为 jpeg
		{"jpeg", "image/jpeg", true},
		{"JPEG", "image/jpeg", true},   // 大小写无关
		{" webp ", "image/webp", true}, // 去空白
		{"png", "image/jpeg", false},
		{"", "image/png", false}, // 空格式不匹配
		{"webp", "image/png", false},
	}
	for _, tc := range cases {
		if got := formatMatchesContentType(tc.format, tc.ct); got != tc.want {
			t.Errorf("formatMatchesContentType(%q,%q)=%v want %v", tc.format, tc.ct, got, tc.want)
		}
	}
}

// ---- 纯逻辑：isBackgroundColor ----

func TestIsBackgroundColor(t *testing.T) {
	// 背景取白色（16 位 65535）。
	var bgR, bgG, bgB uint32 = 65535, 65535, 65535
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	nearWhite := color.RGBA{R: 250, G: 250, B: 250, A: 255} // 差值 ~1285 < 阈值 9800

	if !isBackgroundColor(white, bgR, bgG, bgB) {
		t.Error("white should match white background")
	}
	if isBackgroundColor(black, bgR, bgG, bgB) {
		t.Error("black should not match white background")
	}
	if !isBackgroundColor(nearWhite, bgR, bgG, bgB) {
		t.Error("near-white within threshold should match background")
	}
}

// ---- 纯逻辑：flattenImage ----

func TestFlattenImageNil(t *testing.T) {
	if flattenImage(nil) != nil {
		t.Fatal("flattenImage(nil) should be nil")
	}
}

func TestFlattenImageAlreadyOrigin(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if got := flattenImage(img); got != image.Image(img) {
		t.Fatal("flattenImage should return the same image when already at origin")
	}
}

func TestFlattenImageNormalizesOffset(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 20, 20))
	parent.Set(5, 5, color.RGBA{R: 255, A: 255}) // 红点在子图原点
	sub := parent.SubImage(image.Rect(5, 5, 15, 15))
	if sub.Bounds().Min.X != 5 || sub.Bounds().Min.Y != 5 {
		t.Fatalf("precondition: subimage should have non-zero origin, got %+v", sub.Bounds())
	}
	flat := flattenImage(sub)
	b := flat.Bounds()
	if b.Min.X != 0 || b.Min.Y != 0 {
		t.Fatalf("flattened image should start at (0,0), got %+v", b)
	}
	if b.Dx() != 10 || b.Dy() != 10 {
		t.Fatalf("expected 10x10 flattened image, got %dx%d", b.Dx(), b.Dy())
	}
	// 原 (5,5) 的红点应平移到 (0,0)。
	r, _, _, a := flat.At(0, 0).RGBA()
	if r == 0 || a == 0 {
		t.Fatalf("expected red pixel preserved at (0,0), got r=%d a=%d", r, a)
	}
}

// ---- 纯逻辑：autoCropImage ----

func TestAutoCropImageTrimsBorder(t *testing.T) {
	// 40x40：白边包裹 (10,10)-(30,30) 的深色内容块。
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			if x >= 10 && x < 30 && y >= 10 && y < 30 {
				img.Set(x, y, color.RGBA{R: 10, G: 10, B: 10, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	cropped := autoCropImage(img)
	b := cropped.Bounds()
	if b.Dx() != 20 || b.Dy() != 20 {
		t.Fatalf("expected cropped 20x20 content, got %dx%d (bounds %+v)", b.Dx(), b.Dy(), b)
	}
}

func TestAutoCropImageKeepsTinyImages(t *testing.T) {
	// 小于 10x10 直接原样返回。
	img := image.NewRGBA(image.Rect(0, 0, 6, 6))
	if got := autoCropImage(img); got.Bounds() != img.Bounds() {
		t.Fatalf("tiny image should be returned unchanged, got %+v", got.Bounds())
	}
}
