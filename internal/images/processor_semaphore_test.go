// 业务说明：本文件验证软件转码并发信号量（L82）：InitProcessor 正确初始化上限，且并发的软件缩放/编码
// 请求都能在信号量约束下完成、不死锁。
package images

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"
	"testing"
	"time"
)

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func TestInitProcessorInitializesSoftwareSemaphore(t *testing.T) {
	InitProcessor(2)
	swPtr := softwareSemaphore.Load()
	if swPtr == nil {
		t.Fatal("softwareSemaphore not initialized after InitProcessor")
	}
	if got, want := cap(*swPtr), runtime.NumCPU(); got != want {
		t.Fatalf("software semaphore cap = %d, want NumCPU %d", got, want)
	}
}

func TestProcessImageConcurrentSoftwareEncodesComplete(t *testing.T) {
	InitProcessor(1) // AI 并发=1；软件并发上限=NumCPU
	src := makeTestPNG(t, 64, 64)

	const n = 48
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, ct, err := ProcessImage(src, "image/png", ProcessOptions{Width: 24, Height: 24, Format: "jpeg"})
			if err != nil {
				errs <- err
				return
			}
			if len(out) == 0 || ct != "image/jpeg" {
				errs <- fmt.Errorf("unexpected output: len=%d ct=%s", len(out), ct)
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent ProcessImage did not complete (possible software semaphore deadlock)")
	}
	close(errs)
	for err := range errs {
		t.Errorf("ProcessImage failed: %v", err)
	}
}
