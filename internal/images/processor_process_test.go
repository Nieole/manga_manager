// 本文件是业务回归测试，属于图片处理链路，验证 ProcessImage 的透传短路、缩放尺寸、格式继承/转换、
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

// ---- 滤镜在没有目标尺寸时是空转 ----

// TestProcessImageFilterWithoutResizePassesThrough 守「不假装干活」这条不变量。
//
// 阅读器按 CSS 把图缩到容器，从不下发 w/h，服务端因此不知道该缩到多大。此时重采样滤镜
// 没有任何可做的事——resize 与 imaging 在目标尺寸等于源尺寸时都原样返回。破了的表现是
// 每页白付一次完整解码 + 重编码，还丢掉原始字节透传，而画面与不选滤镜逐字节相同。
func TestProcessImageFilterWithoutResizePassesThrough(t *testing.T) {
	// 用 JPEG 源：重编码一定会改变字节，透传与否分得开。
	src := makeTestJPEG(t, 64, 64)
	for _, filter := range []string{"lanczos3", "bicubic", "mitchell", "lanczos2", "bspline", "catmullrom"} {
		t.Run(filter, func(t *testing.T) {
			out, ct, err := ProcessImage(src, "image/jpeg", ProcessOptions{Filter: filter})
			if err != nil {
				t.Fatalf("ProcessImage failed: %v", err)
			}
			if ct != "image/jpeg" {
				t.Fatalf("expected content type unchanged, got %s", ct)
			}
			if !bytes.Equal(out, src) {
				t.Fatalf("filter %s 没有目标尺寸时必须透传原始字节，实际拿到重编码结果（%d 字节，源 %d 字节）", filter, len(out), len(src))
			}
		})
	}
}

// TestProcessImageFilterAppliesWhenResizing 守住另一半：给了目标尺寸，滤镜就必须真的生效。
func TestProcessImageFilterAppliesWhenResizing(t *testing.T) {
	// 源图带高频花纹，否则平滑渐变经不同插值核可能落到同一批像素上。
	src := avifTestSource(t, 64, 64)
	lanczos, _, err := ProcessImage(src, "image/png", ProcessOptions{Filter: "lanczos3", Width: 37, Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage lanczos3 failed: %v", err)
	}
	bilinear, _, err := ProcessImage(src, "image/png", ProcessOptions{Filter: "bilinear", Width: 37, Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage bilinear failed: %v", err)
	}
	if bytes.Equal(lanczos, bilinear) {
		t.Fatal("lanczos3 与 bilinear 缩到同一尺寸不应得到相同字节——滤镜没有生效")
	}
	if w, _, _ := decodeConfigDims(t, lanczos); w != 37 {
		t.Fatalf("expected width 37, got %d", w)
	}
}

// TestProcessImageFitInsidePreservesAspect 守「框」这条语义：阅读器的适应模式给的是容器这个框，
// 缩出来的页必须等比装进去，而不是被拉成框的形状。
func TestProcessImageFitInsidePreservesAspect(t *testing.T) {
	src := makeTestPNG(t, 600, 900)
	out, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: 512, Height: 512, FitInside: true, Filter: "lanczos3", Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage fit-inside failed: %v", err)
	}
	if w, h, _ := decodeConfigDims(t, out); w != 341 || h != 512 {
		t.Fatalf("expected 341x512 inside a 512x512 box, got %dx%d", w, h)
	}

	// 不设 FitInside 时仍是「画布」语义：封面与缩略图靠它精确出图。
	exact, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: 512, Height: 512, Filter: "lanczos3", Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage exact resize failed: %v", err)
	}
	if w, h, _ := decodeConfigDims(t, exact); w != 512 || h != 512 {
		t.Fatalf("expected exact 512x512 without FitInside, got %dx%d", w, h)
	}
}

// TestProcessImageFitInsideDoesNotUpscale 守「不放大」：框比源图大时不该编出一张放大的图，
// 那只是白付编码与带宽，浏览器自己放大的结果一模一样。
func TestProcessImageFitInsideDoesNotUpscale(t *testing.T) {
	src := makeTestPNG(t, 200, 300)
	out, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: 2048, Height: 2048, FitInside: true, Filter: "lanczos3", Format: "png"})
	if err != nil {
		t.Fatalf("ProcessImage failed: %v", err)
	}
	if w, h, _ := decodeConfigDims(t, out); w != 200 || h != 300 {
		t.Fatalf("expected source size kept, got %dx%d", w, h)
	}
}

// TestProcessImageImagingFiltersWithSingleDimension 守 bspline / catmullrom 只给一条边时不出空图。
//
// imaging.Fit 对任一边 <= 0 直接返回 &image.NRGBA{}——阅读器的「适应宽度 / 适应高度」正好只给
// 一条边，缺了 fitBox 这一步，这两个滤镜交给用户的是整页空白。
func TestProcessImageImagingFiltersWithSingleDimension(t *testing.T) {
	src := avifTestSource(t, 600, 900)
	for _, filter := range []string{"bspline", "catmullrom"} {
		t.Run(filter+"/width only", func(t *testing.T) {
			out, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: 256, Filter: filter, Format: "png"})
			if err != nil {
				t.Fatalf("ProcessImage failed: %v", err)
			}
			if w, h, _ := decodeConfigDims(t, out); w != 256 || h != 384 {
				t.Fatalf("expected 256x384, got %dx%d", w, h)
			}
		})
		t.Run(filter+"/height only", func(t *testing.T) {
			out, _, err := ProcessImage(src, "image/png", ProcessOptions{Height: 300, Filter: filter, Format: "png"})
			if err != nil {
				t.Fatalf("ProcessImage failed: %v", err)
			}
			if w, h, _ := decodeConfigDims(t, out); w != 200 || h != 300 {
				t.Fatalf("expected 200x300, got %dx%d", w, h)
			}
		})
	}
}

// TestSnapTargetDimension 守档位闸：尺寸由客户端算出，服务端不能只信前端已经取过整。
func TestSnapTargetDimension(t *testing.T) {
	cases := map[int]int{
		0:    0,
		1:    ReaderSizeStep,
		256:  256,
		257:  512,
		1100: 1280,
		1250: 1280,
		1280: 1280,
		1400: 1536,
		3072: MaxReaderDimension,
		7000: MaxReaderDimension,
	}
	for in, want := range cases {
		if got := SnapTargetDimension(in); got != want {
			t.Errorf("SnapTargetDimension(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestFilterChangesPixels(t *testing.T) {
	cases := []struct {
		filter        string
		width, height int
		want          bool
	}{
		{"", 0, 0, false},
		{"", 200, 0, false},
		{"lanczos3", 0, 0, false},  // 无尺寸 → 恒等重采样
		{"lanczos3", 200, 0, true}, // 有尺寸 → 真缩放
		{"catmullrom", 0, 300, true},
		{" LANCZOS3 ", 0, 0, false}, // 大小写与空白无关
		{"waifu2x", 0, 0, true},     // AI 放大不需要目标尺寸也会改像素
		{"realcugan", 0, 0, true},
		{"ncnn", 0, 0, true},
	}
	for _, tc := range cases {
		if got := FilterChangesPixels(tc.filter, tc.width, tc.height); got != tc.want {
			t.Errorf("FilterChangesPixels(%q,%d,%d)=%v want %v", tc.filter, tc.width, tc.height, got, tc.want)
		}
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

// TestProcessImageRejectsUnsafeTargetDimensions 锁住输出画布的尺寸闸门。
//
// 解码侧的 maxDecodePixels 只约束「源图有多大」，管不住「要输出多大」——目标画布由调用方
// 的 Width/Height 决定。负值经 uint() 转换会回绕成天文数字，超大值会让 resize 直接申请
// 数 GB 缓冲，任一都能让单次请求打爆进程。
func TestProcessImageRejectsUnsafeTargetDimensions(t *testing.T) {
	src := makeTestPNG(t, 8, 8)

	cases := []struct {
		name          string
		width, height int
	}{
		{"negative width wraps around on uint conversion", -1, 0},
		{"negative height wraps around on uint conversion", 0, -1},
		{"width beyond single-side limit", MaxTargetDimension + 1, 0},
		{"height beyond single-side limit", 0, MaxTargetDimension + 1},
		{"area beyond budget despite legal sides", MaxTargetDimension, MaxTargetDimension},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: tc.width, Height: tc.height}); err == nil {
				t.Fatalf("expected ProcessImage to reject %dx%d", tc.width, tc.height)
			}
		})
	}
}

func TestProcessImageAcceptsReasonableTargetDimensions(t *testing.T) {
	src := makeTestPNG(t, 8, 8)
	if _, _, err := ProcessImage(src, "image/png", ProcessOptions{Width: 4, Height: 4}); err != nil {
		t.Fatalf("expected ordinary resize to succeed, got %v", err)
	}
}

func TestValidateTargetDimensionsAllowsUnspecified(t *testing.T) {
	if err := ValidateTargetDimensions(0, 0); err != nil {
		t.Fatalf("expected 0x0 (unspecified) to be allowed, got %v", err)
	}
}

// TestNormalizeWaifu2xParams 锁住 AI 放大参数的白名单。
//
// 这三个值来自 HTTP 查询串，Waifu2xFormat 会被拼进沙盒输出路径
// （filepath.Join(sandboxDir, "out."+format)）与子进程 argv，未归一化时
// "../../../tmp/x" 可让引擎把文件写到沙盒之外。
func TestNormalizeWaifu2xParams(t *testing.T) {
	formatCases := map[string]string{
		"":                  "webp",
		"webp":              "webp",
		"PNG":               "png",
		"jpeg":              "jpg",
		"jpg":               "jpg",
		"../../../tmp/evil": "webp",
		"png/../../etc":     "webp",
		"exe":               "webp",
	}
	for in, want := range formatCases {
		if got := normalizeWaifu2xFormat(in); got != want {
			t.Fatalf("normalizeWaifu2xFormat(%q) = %q, want %q", in, got, want)
		}
	}

	scaleCases := map[int]int{0: 2, 1: 1, 2: 2, 3: 2, 4: 4, 8: 2, -1: 2, 99999: 2}
	for in, want := range scaleCases {
		if got := normalizeWaifu2xScale(in); got != want {
			t.Fatalf("normalizeWaifu2xScale(%d) = %d, want %d", in, got, want)
		}
	}

	noiseCases := map[int]int{-5: -1, -1: -1, 0: 0, 3: 3, 99: 3}
	for in, want := range noiseCases {
		if got := normalizeWaifu2xNoise(in); got != want {
			t.Fatalf("normalizeWaifu2xNoise(%d) = %d, want %d", in, got, want)
		}
	}
}
