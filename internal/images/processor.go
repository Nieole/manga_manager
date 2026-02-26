package images

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	Width         int
	Height        int
	Format        string // webp, jpeg, png
	Quality       int    // 0-100
	Filter        string // bicubic, lanczos3, waifu2x, ncnn
	Waifu2xPath   string // 允许动态指定引擎启动文件路径
	RealCuganPath string // 允许动态指定 realcugan 引擎启动文件路径
	Waifu2xScale  int    // 引擎缩放倍数 1/2/4/8
	Waifu2xNoise  int    // Waifu2x 的降噪等级 / RealCUGAN 的噪点抑制强度
	Waifu2xFormat string // 降噪外设输出格式 webp/png/jpg
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

	// 针对 Waifu2x / realcugan / ncnn 这种需要外部挂载文件系统的超分辨率算法单独开一条短路通道
	if opts.Filter == "waifu2x" || opts.Filter == "realcugan" || opts.Filter == "ncnn" {
		outData, err := execWaifu2x(data, contentType, opts)
		if err == nil {
			// 直接返回加工好的 原始字节数组
			// 为了防止前端不认识，强制重置 contentType
			contentType := "image/png"
			if opts.Waifu2xFormat != "" {
				if opts.Waifu2xFormat == "jpg" || opts.Waifu2xFormat == "jpeg" {
					contentType = "image/jpeg"
				} else {
					contentType = "image/" + opts.Waifu2xFormat
				}
			}
			return outData, contentType, nil
		}
		// 如果 waifu2x 执行失败，退回到下面原生的 Lanczos 软算逻辑
		slog.Warn("Waifu2x execution failed. Falling back to Lanczos3.", "error", err)
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
func execWaifu2x(imgData []byte, contentType string, opts ProcessOptions) ([]byte, error) {
	var execPath string
	binName := "waifu2x-ncnn-vulkan"
	if opts.Filter == "realcugan" {
		binName = "realcugan-ncnn-vulkan"
	}

	// 判断是否启用了自定义引擎路径机制
	customPath := opts.Waifu2xPath
	if opts.Filter == "realcugan" {
		customPath = opts.RealCuganPath
	}

	if customPath != "" {
		if _, err := os.Stat(customPath); os.IsNotExist(err) {
			slog.Warn("Custom engine path specified but not found on disk", "custom_path", customPath)
			// 退火等待全局嗅探
		} else {
			execPath = customPath
		}
	}

	// 如果自定义路径为空，或者文件不存在被退回，走原本的动态联排机制
	if execPath == "" {
		// 组装依据底层操作系统构架动态映射的 执行终端文件名
		if runtime.GOOS == "windows" {
			binName += ".exe"
		}

		// 智能多级寻址：先检查是否安装于系统的环境变量（无需携带路径），再检查内附的 bin/ 底下
		if _, err := exec.LookPath(binName); err == nil {
			execPath = binName // 可以直接执行，它在 PATH 环境变量里
		} else {
			// 在内置文件夹中搜刮
			localPath := filepath.Join(".", "bin", "waifu2x", binName)
			if _, localErr := os.Stat(localPath); os.IsNotExist(localErr) {
				return nil, fmt.Errorf("waifu2x binary not found globally nor at local path %s", localPath)
			}
			execPath = localPath
		}
	}

	// 建立系统临时目录工作空间作为严格干净的沙盒
	sandboxDir, err := os.MkdirTemp("", "waifu_sandbox_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(sandboxDir)

	// 根据原始图片的 MIME 类型推断正确的输入文件扩展名
	inExt := "jpg"
	switch {
	case strings.Contains(contentType, "png"):
		inExt = "png"
	case strings.Contains(contentType, "webp"):
		inExt = "webp"
	case strings.Contains(contentType, "gif"):
		inExt = "gif"
	case strings.Contains(contentType, "bmp"):
		inExt = "bmp"
	case strings.Contains(contentType, "avif"):
		inExt = "avif"
	}
	inPath := filepath.Join(sandboxDir, "in."+inExt)

	outExt := "webp" // default fallback
	if opts.Waifu2xFormat != "" {
		outExt = strings.ToLower(opts.Waifu2xFormat)
		if outExt == "jpeg" {
			outExt = "jpg"
		}
	}
	outPath := filepath.Join(sandboxDir, "out."+outExt)

	// 写原数据到 in.jpg
	if err := os.WriteFile(inPath, imgData, 0644); err != nil {
		return nil, err
	}

	// 组装 NCNN-Vulkan 家族系列执行命令
	// -s : 倍数放大
	// -n : 降噪
	// -f <ext> : 输出全画幅指定的格式
	// 规避找不到模型导致的空指针 Segment Fault 闪退
	// 将工作目录（Cwd）锁死为引擎所在目录（不论是内部引用、环境寻找、还是用户指定）
	absExecPath, err := filepath.Abs(execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for waifu2x binary: %w", err)
	}
	execDir := filepath.Dir(absExecPath)

	// 提取从前端下发的客制化倍率，如未显性指示则分别跌落至默认倍数 2, 降噪 0
	scaleStr := "2"
	if opts.Waifu2xScale > 0 {
		scaleStr = strconv.Itoa(opts.Waifu2xScale)
	}
	noiseStr := "0"
	if opts.Waifu2xNoise >= -1 {
		noiseStr = strconv.Itoa(opts.Waifu2xNoise)
	}

	cmd := exec.Command(execPath, "-i", inPath, "-o", outPath, "-s", scaleStr, "-n", noiseStr, "-f", outExt)
	cmd.Dir = execDir // 指定子进程在其引擎本体所在文件夹起飞！

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s execution failed: %v, output: %s", binName, err, string(output))
	}
	slog.Info("AI upscaling execution successful", "engine", binName, "output_snippet", string(output[:min(len(output), 100)]))

	// 读取处理完毕的磁盘输出图
	processedData, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read waifu2x output: %v", err)
	}

	return processedData, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
