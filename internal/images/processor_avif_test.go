// 本文件守 avif 编码参数必须显式给出这条不变量。avif.Options 的 Speed 与 ChromaSubsampling
// 两个字段的 Go 零值在库里都不是「未指定」，漏写 Speed 会让每张缩略图掉进最慢档，
// 400px 封面从毫秒级变成几十秒，千本库的封面阶段从分钟级变成小时级。

package images

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/gen2brain/avif"
	"github.com/nfnt/resize"
)

// avifTestSource 生成一张带渐变与色块的 PNG，避免纯色图被编码器压到无法体现档位差异。
func avifTestSource(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / w),
				G: uint8((y * 255) / h),
				B: uint8(((x ^ y) * 3) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode source png: %v", err)
	}
	return buf.Bytes()
}

func TestAvifEncodeOptionsAreExplicit(t *testing.T) {
	t.Run("库里的零值不等于未指定", func(t *testing.T) {
		// 这两条钉住的是本文件存在的理由：库对 Quality 判 <=0 回落默认值，对 Speed 只判 <0，
		// 于是 Speed 的零值 0 被当成「显式选了最慢档」；ChromaSubsampling 的零值是 4:4:4，
		// 而库默认 4:2:0。哪天库把零值也当未指定，这里会红，届时可以重新评估显式赋值。
		var zero avif.Options
		if zero.Speed != 0 {
			t.Fatalf("avif.Options 零值 Speed = %d，预期 0", zero.Speed)
		}
		if zero.ChromaSubsampling != image.YCbCrSubsampleRatio444 {
			t.Fatalf("avif.Options 零值色度 = %v，预期 4:4:4", zero.ChromaSubsampling)
		}
	})

	t.Run("投递档显式给出档位与 4:2:0 色度", func(t *testing.T) {
		opts := avifDeliveryOptions(82)
		if opts.Speed < 1 || opts.Speed > 10 {
			t.Fatalf("投递档 Speed = %d，必须显式落在 [1,10]", opts.Speed)
		}
		if opts.ChromaSubsampling != image.YCbCrSubsampleRatio420 {
			t.Fatalf("投递档色度 = %v，预期 4:2:0", opts.ChromaSubsampling)
		}
		if opts.Quality != 82 {
			t.Fatalf("投递档 Quality = %d，预期透传 82", opts.Quality)
		}
	})

	t.Run("放大中间态显式给出档位与 4:4:4 色度", func(t *testing.T) {
		opts := avifIntermediateOptions()
		if opts.Speed < 1 || opts.Speed > 10 {
			t.Fatalf("中间态 Speed = %d，必须显式落在 [1,10]", opts.Speed)
		}
		if opts.ChromaSubsampling != image.YCbCrSubsampleRatio444 {
			t.Fatalf("中间态色度 = %v，预期 4:4:4", opts.ChromaSubsampling)
		}
		if opts.Quality != 100 {
			t.Fatalf("中间态 Quality = %d，预期 100", opts.Quality)
		}
	})
}

// TestProcessImageAvifThumbnailStaysFast 守 ProcessImage 这条实际调用链，挡住绕开
// avifDeliveryOptions 直接写 avif.Options 字面量的回归。判据是同机同图的相对倍数而非绝对秒数：
// 基准是同一张 400px 图用库自带最快档现编一次，与被测跑在同一台机器上，机器快慢自行抵消，
// CI 性能不定不影响判定。
func TestProcessImageAvifThumbnailStaysFast(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过计时用例")
	}
	source := avifTestSource(t, 1200, 1800)

	// 基准图取 ProcessImage 内部同一步缩放的产物，保证两边编码的是同一张图、只差档位。
	decoded, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		t.Fatalf("解码基准源图失败: %v", err)
	}
	baselineImg := resize.Resize(400, 0, decoded, resize.Bilinear)

	// 先空跑一次：首次调用要付 WASM 模块实例化的一次性开销，落在谁头上都会歪曲倍数。
	if err := avif.Encode(io.Discard, image.NewRGBA(image.Rect(0, 0, 32, 32)), avifDeliveryOptions(82)); err != nil {
		t.Fatalf("预热编码器失败: %v", err)
	}

	var baselineBuf bytes.Buffer
	baseStart := time.Now()
	if err := avif.Encode(&baselineBuf, baselineImg, avif.Options{Quality: 82, Speed: 10, ChromaSubsampling: image.YCbCrSubsampleRatio420}); err != nil {
		t.Fatalf("编码基准图失败: %v", err)
	}
	baseline := time.Since(baseStart)

	start := time.Now()
	out, contentType, err := ProcessImage(source, "image/png", ProcessOptions{Width: 400, Quality: 82, Format: "avif"})
	if err != nil {
		t.Fatalf("ProcessImage avif 失败: %v", err)
	}
	elapsed := time.Since(start)

	if contentType != "image/avif" {
		t.Fatalf("Content-Type = %s，预期 image/avif", contentType)
	}
	if len(out) == 0 {
		t.Fatal("avif 缩略图为空")
	}

	// 显式档位加上解码与缩放的固定开销只在个位数倍；零值最慢档比最快档慢三个数量级。
	// 上限取 50 倍：离显式档位有一位数的余量，离最慢档还差一个数量级以上。
	const maxRatio = 50
	if elapsed > baseline*maxRatio {
		t.Fatalf("ProcessImage avif 耗时 %v，超过最快档基准 %v 的 %d 倍——Speed 很可能落回了零值最慢档", elapsed, baseline, maxRatio)
	}
	t.Logf("avif 400px 缩略图 %v（最快档基准 %v，%d 字节）", elapsed, baseline, len(out))
}
