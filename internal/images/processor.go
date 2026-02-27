package images

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
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

	"image/color"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
)

// 全局 AI 并发控制信号量，防止瞬间拉起过多引擎进程撑爆 CPU/GPU/RAM
var aiSemaphore chan struct{}

// InitProcessor 初始化处理器全局参数
func InitProcessor(maxAiConcurrency int) {
	if maxAiConcurrency <= 0 {
		maxAiConcurrency = 1
	}
	aiSemaphore = make(chan struct{}, maxAiConcurrency)
}

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
	AutoCrop      bool   // 是否自动裁切白边
}

func ProcessImage(data []byte, contentType string, opts ProcessOptions) ([]byte, string, error) {
	// 如果没有任何处理需求且不需要裁切，直接短路透传原始数据
	if opts.Width == 0 && opts.Height == 0 && opts.Filter == "" && !opts.AutoCrop {
		return data, contentType, nil
	}

	// 性能优化：预检图片配置而不完全解码，用于评估内存风险
	readerConfig := bytes.NewReader(data)
	if config, _, err := image.DecodeConfig(readerConfig); err == nil {
		// 内存保护阈值：如果图片面积超过 2500 万像素 (如 5000x5000)
		// 在没有大并发处理能力的小型服务器上，完全解压这种位图会瞬间消耗 ~100MB+ 内存
		area := config.Width * config.Height
		if area > 25000000 {
			slog.Warn("Large image detected, using memory-safe processing path", "width", config.Width, "height", config.Height, "area", area)
			// 未来可在此处实施下采样读取或直接拒绝过大请求以防止 OOM
		}
	}

	img, _, err := decodeImage(data, contentType)
	if err != nil {
		return nil, "", fmt.Errorf("decode image err: %w", err)
	}

	var newImg image.Image = img

	// 自动裁切白边逻辑
	if opts.AutoCrop {
		newImg = autoCropImage(newImg)
		// 重要：裁切后的 SubImage 可能带有非零的 Min.X/Y 和原始父图的步长(Stride)
		// 这会导致某些编码器(如 cgo 封装的库)出现偏移、斜切或花屏
		// 必须执行归一化，将其绘制到一个全新的从 (0,0) 开始的干净画布中
		newImg = flattenImage(newImg)
	}

	targetWidth := uint(opts.Width)
	targetHeight := uint(opts.Height)

	// 如果前端要求了滤镜但没有缩放，强制按照原始大小执行一次采样插值洗流
	if (opts.Filter != "" && opts.Filter != "nearest" && opts.Filter != "average" && opts.Filter != "bilinear") && targetWidth == 0 && targetHeight == 0 {
		targetWidth = uint(newImg.Bounds().Dx())
		targetHeight = uint(newImg.Bounds().Dy())
	}

	// 针对 Waifu2x / realcugan / ncnn 这种需要外部挂载文件系统的超分辨率算法单独开一条短路通道
	if opts.Filter == "waifu2x" || opts.Filter == "realcugan" || opts.Filter == "ncnn" {
		outData, err := execWaifu2x(newImg, data, contentType, opts)
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
		switch opts.Filter {
		case "mitchell":
			newImg = resize.Resize(targetWidth, targetHeight, newImg, resize.MitchellNetravali)
		case "lanczos2":
			newImg = resize.Resize(targetWidth, targetHeight, newImg, resize.Lanczos2)
		case "bspline":
			newImg = imaging.Fit(newImg, int(targetWidth), int(targetHeight), imaging.BSpline)
		case "catmullrom":
			newImg = imaging.Fit(newImg, int(targetWidth), int(targetHeight), imaging.CatmullRom)
		default:
			var interp resize.InterpolationFunction = resize.Bilinear
			switch opts.Filter {
			case "bicubic":
				interp = resize.Bicubic
			case "lanczos3":
				interp = resize.Lanczos3
			case "nearest":
				interp = resize.NearestNeighbor
			}
			newImg = resize.Resize(targetWidth, targetHeight, newImg, interp)
		}
	}

	var buf bytes.Buffer
	var newContentType string

	format := strings.ToLower(opts.Format)
	if format == "" {
		// 如果未显式指定目标格式，则尝试从原始 contentType 中继承，避免非必要转换
		if strings.Contains(contentType, "webp") {
			format = "webp"
		} else if strings.Contains(contentType, "png") {
			format = "png"
		} else if strings.Contains(contentType, "avif") {
			format = "avif"
		} else {
			format = "jpeg" // 兜底格式
		}
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
func execWaifu2x(img image.Image, rawData []byte, contentType string, opts ProcessOptions) ([]byte, error) {
	// 获取信号量锁 (Semaphore Acquire)
	// 如果由于读页并发过高，此处会阻塞协程直到前序 AI 任务完成
	if aiSemaphore != nil {
		aiSemaphore <- struct{}{}
		defer func() { <-aiSemaphore }() // 放锁 (Semaphore Release)
	}

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

	// 将图片状态落盘。如果图片已经在内存中被 ProcessImage 裁切过（且已执行归一化），则使用原始图片格式重新编码；
	// 如果没有任何内存变动，则直接使用原始字节流以追求极致效率。
	if img != nil && opts.AutoCrop {
		f, err := os.Create(inPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		// 智能识别原始格式并选择最匹配的编码器作为中间件，绝不跨格式转换
		if strings.Contains(contentType, "webp") {
			// 使用无损 WebP 作为中间件
			err = webp.Encode(f, img, &webp.Options{Lossless: true})
		} else if strings.Contains(contentType, "png") {
			err = png.Encode(f, img)
		} else if strings.Contains(contentType, "avif") {
			err = avif.Encode(f, img, avif.Options{Quality: 100})
		} else {
			// JPEG 情况，使用最高质量保存中间状态
			err = jpeg.Encode(f, img, &jpeg.Options{Quality: 100})
		}

		if err != nil {
			return nil, err
		}
	} else {
		if err := os.WriteFile(inPath, rawData, 0644); err != nil {
			return nil, err
		}
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

// autoCropImage 扫描图像边缘，识别并裁切掉与背景色相近的边界白边/黑边
func autoCropImage(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width < 10 || height < 10 {
		return img
	}

	// 采样背景色（通常取左上角，但也考虑边缘多点采样以提高鲁棒性）
	bgR, bgG, bgB, _ := img.At(bounds.Min.X, bounds.Min.Y).RGBA()

	// 寻找内容的上下左右边界
	top, bottom, left, right := 0, height-1, 0, width-1

	// 自顶向下扫描
	found := false
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !isBackgroundColor(img.At(bounds.Min.X+x, bounds.Min.Y+y), bgR, bgG, bgB) {
				top = y
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	// 自底向上扫描
	found = false
	for y := height - 1; y >= top; y-- {
		for x := 0; x < width; x++ {
			if !isBackgroundColor(img.At(bounds.Min.X+x, bounds.Min.Y+y), bgR, bgG, bgB) {
				bottom = y
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	// 自左向右扫描
	found = false
	for x := 0; x < width; x++ {
		for y := top; y <= bottom; y++ {
			if !isBackgroundColor(img.At(bounds.Min.X+x, bounds.Min.Y+y), bgR, bgG, bgB) {
				left = x
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	// 自右向左扫描
	found = false
	for x := width - 1; x >= left; x-- {
		for y := top; y <= bottom; y++ {
			if !isBackgroundColor(img.At(bounds.Min.X+x, bounds.Min.Y+y), bgR, bgG, bgB) {
				right = x
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	// 如果裁切范围太小或者干脆没变，直接返回原图
	if !found || (right-left < 10) || (bottom-top < 10) {
		return img
	}

	// 执行子图裁切
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}

	if si, ok := img.(subImager); ok {
		return si.SubImage(image.Rect(bounds.Min.X+left, bounds.Min.Y+top, bounds.Min.X+right+1, bounds.Min.Y+bottom+1))
	}

	return img
}

// isBackgroundColor 判断给定颜色是否属于背景色范畴。引入阈值处理以应对 JPEG 边缘噪点。
func isBackgroundColor(c color.Color, bgR, bgG, bgB uint32) bool {
	r, g, b, _ := c.RGBA()

	// 阈值设为 15% (由于 RGBA 是 16位 0-65535，15% 大约是 9800)
	const threshold uint32 = 9800

	diff := func(a, b uint32) uint32 {
		if a > b {
			return a - b
		}
		return b - a
	}

	return diff(r, bgR) < threshold && diff(g, bgG) < threshold && diff(b, bgB) < threshold
}

// flattenImage 将可能带有偏移坐标（SubImage 产生）的图像归一化
// 强制将图像绘制到一个起始坐标为 (0,0) 且内存排布完全紧凑的新 Canvas 中
// 从而消除编码器在处理 Stride 或 Bounds.Min 时的兼容性问题（防花屏）
func flattenImage(img image.Image) image.Image {
	if img == nil {
		return nil
	}

	bounds := img.Bounds()
	// 如果已经是标准 (0,0) 起始，通常不需要重绘，但在处理裁切图时，为保险起见建议总是重绘以优化 Stride
	if bounds.Min.X == 0 && bounds.Min.Y == 0 {
		return img
	}

	width := bounds.Dx()
	height := bounds.Dy()

	// 根据原始图像是否有 Alpha 通道选择合适的画布类型
	var canvas draw.Image
	switch img.(type) {
	case *image.NRGBA, *image.RGBA:
		canvas = image.NewNRGBA(image.Rect(0, 0, width, height))
	default:
		// 默认使用 NRGBA 以获得更好的通用性和 Alpha 处理
		canvas = image.NewNRGBA(image.Rect(0, 0, width, height))
	}

	// 执行重绘，将内容平移至 (0,0)
	draw.Draw(canvas, canvas.Bounds(), img, bounds.Min, draw.Src)
	return canvas
}
