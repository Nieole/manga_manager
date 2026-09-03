// 本文件守「交给编码器的图必须行间紧凑」这条不变量。裁切出的 SubImage 继承父图的 Stride，
// 按 Pix 整段直读的编码器会逐行错位读出整页花屏，而坏字节还会被上游写进内存与磁盘缓存。
// 破了的表现是：开自动裁边读 avif 页，整页颜色错乱，清缓存前刷新也不恢复。

package images

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/avif"
)

// strideTolerance 是解码回来的像素与真值的平均绝对误差上限（通道值域 0-65535）。
// 取 1500（约 2.3%）：有损编码与色彩空间往返落在几十到几百，而行错位会打到上万。
const strideTolerance = 1500

// gradientImage 画一张双向渐变：横向变红、纵向变绿。渐变对有损编码器友好（误差本底低），
// 又让行错位无所遁形——错开一行就是一大截色差。
func gradientImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 96, A: 255})
		}
	}
	return img
}

// meanChannelError 比较两图左上角对齐的 w x h 区域，返回 RGB 三通道的平均绝对误差。
func meanChannelError(got, want image.Image, w, h int) float64 {
	gb, wb := got.Bounds(), want.Bounds()
	diff := func(a, b uint32) float64 {
		if a > b {
			return float64(a - b)
		}
		return float64(b - a)
	}
	var sum float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r1, g1, b1, _ := got.At(gb.Min.X+x, gb.Min.Y+y).RGBA()
			r2, g2, b2, _ := want.At(wb.Min.X+x, wb.Min.Y+y).RGBA()
			sum += diff(r1, r2) + diff(g1, g2) + diff(b1, b2)
		}
	}
	return sum / float64(w*h*3)
}

// convertTo 把源图重画成指定的具体类型，用于构造各类型的父图。
func convertTo(t *testing.T, kind string, src *image.RGBA) image.Image {
	t.Helper()
	b := src.Bounds()
	if kind == "YCbCr" {
		m := image.NewYCbCr(b, image.YCbCrSubsampleRatio420)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := src.RGBAAt(x, y)
				cy, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
				m.Y[m.YOffset(x, y)] = cy
				m.Cb[m.COffset(x, y)] = cb
				m.Cr[m.COffset(x, y)] = cr
			}
		}
		return m
	}
	var dst draw.Image
	switch kind {
	case "RGBA":
		dst = image.NewRGBA(b)
	case "NRGBA":
		dst = image.NewNRGBA(b)
	case "Gray":
		dst = image.NewGray(b)
	case "CMYK":
		dst = image.NewCMYK(b)
	default:
		t.Fatalf("未知图像类型 %s", kind)
	}
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

// encodeAll 把同一张图喂给四个交付用编码器，返回各自的字节。
func encodeAll(t *testing.T, img image.Image) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, name := range []string{"png", "jpeg", "webp", "avif"} {
		var buf bytes.Buffer
		var err error
		switch name {
		case "png":
			err = png.Encode(&buf, img)
		case "jpeg":
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
		case "webp":
			_, err = encodeWebP(&buf, img, 100, true)
		case "avif":
			err = avif.Encode(&buf, img, avifIntermediateOptions())
		}
		if err != nil {
			t.Fatalf("%s 编码失败: %v", name, err)
		}
		out[name] = buf.Bytes()
	}
	return out
}

// TestFlattenedSubImageSurvivesEveryEncoder 是编码器矩阵：各具体类型的非紧凑子图经
// flattenImage 归一化后，四个编码器都必须还原出正确像素。
//
// 归一化缺位时 avif 会花屏——它对 *image.RGBA 直接拿 Pix 整段读，按「Stride 等于一行字节数」
// 解释缓冲。别的编码器与别的类型目前各自安全，但那是库内部实现，本用例把它们一并钉住。
func TestFlattenedSubImageSurvivesEveryEncoder(t *testing.T) {
	const parentW, parentH = 200, 160
	const subW, subH = 120, 100 // 比父图窄 → 子图 Stride 非紧凑

	pattern := gradientImage(parentW, parentH)
	truth := image.NewRGBA(image.Rect(0, 0, subW, subH))
	draw.Draw(truth, truth.Bounds(), pattern, image.Point{}, draw.Src)

	for _, kind := range []string{"RGBA", "NRGBA", "Gray", "CMYK", "YCbCr"} {
		t.Run(kind, func(t *testing.T) {
			parent := convertTo(t, kind, pattern)
			si, ok := parent.(interface {
				SubImage(image.Rectangle) image.Image
			})
			if !ok {
				t.Fatalf("%s 不支持 SubImage，无法构造非紧凑子图", kind)
			}
			// 左上角落在 (0,0)：Bounds.Min 是干净的，Stride 却仍是父图的。
			sub := si.SubImage(image.Rect(0, 0, subW, subH))
			if sub.Bounds().Min != (image.Point{}) {
				t.Fatalf("前提不成立：子图起点应在 (0,0)，实为 %v", sub.Bounds().Min)
			}
			if hasCompactRows(sub) {
				t.Fatalf("前提不成立：子图应当行间非紧凑，%T 却报告紧凑", sub)
			}

			// 参照物：同类型下真值的紧凑图，抵消类型转换本身带来的误差。
			reference := convertTo(t, kind, truth)

			flat := flattenImage(sub)
			for name, data := range encodeAll(t, flat) {
				decoded, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatalf("%s 解码失败: %v", name, err)
				}
				if decoded.Bounds().Dx() != subW || decoded.Bounds().Dy() != subH {
					t.Fatalf("%s 输出尺寸 %v，预期 %dx%d", name, decoded.Bounds(), subW, subH)
				}
				if mae := meanChannelError(decoded, reference, subW, subH); mae > strideTolerance {
					t.Fatalf("%s 平均绝对误差 %.1f 超过阈值 %d——非紧凑 Stride 未被归一化，行已错位", name, mae, strideTolerance)
				}
			}
		})
	}
}

// TestFlattenImageNormalizesNonCompactStride 单独钉住判据本身：起点已在 (0,0) 不足以说明
// 缓冲是紧凑的，早退必须同时看 Stride。
func TestFlattenImageNormalizesNonCompactStride(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 40, 40))
	sub := parent.SubImage(image.Rect(0, 0, 20, 20)).(*image.RGBA)
	if sub.Bounds().Min != (image.Point{}) {
		t.Fatalf("前提不成立：子图起点应在 (0,0)，实为 %v", sub.Bounds().Min)
	}
	if sub.Stride == 4*sub.Rect.Dx() {
		t.Fatalf("前提不成立：子图 Stride 应为父图的 %d，实为 %d", 4*parent.Rect.Dx(), sub.Stride)
	}

	flat := flattenImage(sub)
	if flat == image.Image(sub) {
		t.Fatal("非紧凑子图必须被重绘，不得原样返回")
	}
	if !hasCompactRows(flat) {
		t.Fatalf("归一化后仍非紧凑: %T", flat)
	}
	if flat.Bounds() != image.Rect(0, 0, 20, 20) {
		t.Fatalf("归一化后 bounds = %v，预期 (0,0)-(20,20)", flat.Bounds())
	}
}

// cropAtOriginPNG 画一张右下留白的页：裁切框左上角会落在 (0,0)，
// 于是子图起点干净、Stride 却是父图的——问题正是从这里进的编码器。
func cropAtOriginPNG(t testing.TB, w, h, border int) []byte {
	t.Helper()
	content := gradientImage(w-border, h-border)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{255, 255, 255, 255}), image.Point{}, draw.Src)
	draw.Draw(img, content.Bounds(), content, image.Point{}, draw.Src)
	// (0,0) 取背景色，但顶行与左列都有内容 → 边缘扫描给出 left=0、top=0。
	img.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码源图失败: %v", err)
	}
	return buf.Bytes()
}

// TestProcessImageAutoCropToAvifKeepsPixels 是端到端用例：裁边 + 不缩放 + 输出 avif，
// 四个触发条件齐备时输出必须仍是原内容。这是用户看到花屏的那条请求。
func TestProcessImageAutoCropToAvifKeepsPixels(t *testing.T) {
	const w, h, border = 300, 400, 40
	source := cropAtOriginPNG(t, w, h, border)
	truth := gradientImage(w-border, h-border)

	out, contentType, err := ProcessImage(source, "image/png", ProcessOptions{AutoCrop: true, Format: "avif", Quality: 90})
	if err != nil {
		t.Fatalf("ProcessImage 失败: %v", err)
	}
	if contentType != "image/avif" {
		t.Fatalf("Content-Type = %s，预期 image/avif", contentType)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("解码输出失败: %v", err)
	}
	if decoded.Bounds().Dx() != w-border || decoded.Bounds().Dy() != h-border {
		t.Fatalf("裁切后尺寸 %v，预期 %dx%d", decoded.Bounds(), w-border, h-border)
	}
	if mae := meanChannelError(decoded, truth, w-border, h-border); mae > strideTolerance {
		t.Fatalf("裁边后 avif 平均绝对误差 %.1f 超过阈值 %d——整页已花屏", mae, strideTolerance)
	}
}
