package images

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chai2010/webp"
	"github.com/nfnt/resize"
	golangWebp "golang.org/x/image/webp"

	// Init defaults for read fallback
	_ "image/gif"

	"github.com/gen2brain/avif"
)

// ProcessOptions 用于接受前端动态要求的尺寸转换
type ProcessOptions struct {
	Width   int
	Height  int
	Format  string // webp, jpeg, png
	Quality int    // 0-100
	Filter  string // bicubic, lanczos3, waifu2x, ncnn
}

func ProcessImage(data []byte, contentType string, opts ProcessOptions) ([]byte, string, error) {
	if opts.Width == 0 && opts.Height == 0 && opts.Filter == "" {
		return data, contentType, nil
	}

	img, _, err := decodeImage(data, contentType)
	if err != nil {
		return nil, "", fmt.Errorf("decode image err: %w", err)
	}

	var newImg image.Image = img

	targetWidth := uint(opts.Width)
	targetHeight := uint(opts.Height)

	// 如果前端要求了滤镜但没有缩放，强制按照原始大小执行一次采样插值洗流
	if (opts.Filter != "" && opts.Filter != "nearest" && opts.Filter != "average" && opts.Filter != "bilinear") && targetWidth == 0 && targetHeight == 0 {
		targetWidth = uint(img.Bounds().Max.X)
		targetHeight = uint(img.Bounds().Max.Y)
	}

	// 针对 Waifu2x / ncnn 这种需要外部挂载文件系统的超分辨率算法单独开一条短路通道
	if opts.Filter == "waifu2x" || opts.Filter == "ncnn" {
		outData, err := execWaifu2x(data, opts)
		if err == nil {
			// 直接返回加工好的 PNG 原始字节数组
			// 为了防止前端不认识，强制重置 contentType
			return outData, "image/png", nil
		}
		// 如果 waifu2x 执行失败，退回到下面原生的 Lanczos 软算逻辑
		fmt.Printf("[Processor] Waifu2x err: %v. Falling back to Lanczos3.\n", err)
		opts.Filter = "lanczos3"
	}

	if targetWidth > 0 || targetHeight > 0 {
		var interp resize.InterpolationFunction = resize.Bilinear
		switch opts.Filter {
		case "bicubic":
			interp = resize.Bicubic
		case "lanczos3":
			interp = resize.Lanczos3
		case "nearest":
			interp = resize.NearestNeighbor
		}
		newImg = resize.Resize(targetWidth, targetHeight, img, interp)
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
	case "avif":
		err = avif.Encode(&buf, newImg, avif.Options{Quality: opts.Quality})
		newContentType = "image/avif"
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

// execWaifu2x 封闭处理 Waifu2x 外部二进制引擎挂载调用、零担内存置换及事后清理
func execWaifu2x(imgData []byte, opts ProcessOptions) ([]byte, error) {
	// 组装依据底层操作系统构架动态映射的 Waifu2x 执行终端文件名
	binName := "waifu2x-ncnn-vulkan"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	// 智能多级寻址：先检查是否安装于系统的环境变量（无需携带路径），再检查内附的 bin/ 底下
	execPath := binName
	if _, err := exec.LookPath(execPath); err != nil {
		execPath = filepath.Join(".", "bin", "waifu2x", binName)
		if _, err := os.Stat(execPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("waifu2x binary not found globally nor at local path %s", execPath)
		}
	}

	// 建立系统临时目录工作空间作为严格干净的沙盒
	sandboxDir, err := os.MkdirTemp("", "waifu_sandbox_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(sandboxDir)

	inPath := filepath.Join(sandboxDir, "in.jpg")
	outPath := filepath.Join(sandboxDir, "out.png")

	// 写原数据到 in.jpg
	if err := os.WriteFile(inPath, imgData, 0644); err != nil {
		return nil, err
	}

	// 组装 Waifu2x-ncnn-vulkan 执行命令
	// -s 2 : 倍数放大2倍
	// -n 1 : 默认等级第一级降噪
	// -f png : 输出全画幅 png 保留
	cmd := exec.Command(execPath, "-i", inPath, "-o", outPath, "-s", "2", "-n", "1", "-f", "png")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execution failed: %v, output: %s", err, string(output))
	}
	fmt.Printf("[Processor] Waifu2x execution successful. Output: %s\n", string(output))

	// 读取处理完毕的磁盘输出图
	processedData, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read waifu2x output: %v", err)
	}

	return processedData, nil
}
