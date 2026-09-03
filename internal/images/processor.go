package images

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nfnt/resize"
	golangWebp "golang.org/x/image/webp"

	// Init defaults for read fallback
	_ "image/gif"

	"image/color"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
)

// 全局 AI 并发控制信号量，防止瞬间拉起过多引擎进程撑爆 CPU/GPU/RAM。
// 用 atomic.Pointer 持有 channel：配置热更新会调用 InitProcessor 重建 channel，
// 若直接替换裸 channel 变量，正在 acquire/release 的请求 goroutine 会读到不同的
// channel（获取旧的、释放到新的），导致令牌错配、goroutine 永久阻塞甚至内存泄漏。
// 调用方必须把 Load() 到的 channel 快照进局部变量，acquire 与 release 用同一引用。
var aiSemaphore atomic.Pointer[chan struct{}]

// softwareSemaphore 封顶纯软件（Go 内解码/缩放/编码）的并发。与 aiSemaphore 用同样的 atomic.Pointer
// 快照约定，避免热更新重建 channel 时令牌错配。上限取 CPU 核数：软件转码是 CPU 密集，超过核数的并发
// 只会引发上下文切换抖动而不提速。
var softwareSemaphore atomic.Pointer[chan struct{}]

// InitProcessor 初始化处理器全局参数
func InitProcessor(maxAiConcurrency int) {
	if maxAiConcurrency <= 0 {
		maxAiConcurrency = 1
	}
	ch := make(chan struct{}, maxAiConcurrency)
	aiSemaphore.Store(&ch)

	softwareLimit := runtime.NumCPU()
	if softwareLimit < 1 {
		softwareLimit = 1
	}
	swCh := make(chan struct{}, softwareLimit)
	softwareSemaphore.Store(&swCh)
}

// AIConcurrency 返回当前 AI 放大子进程的并发上限（未初始化时为 0）。供运行时诊断与配置生效的回归测试观测。
func AIConcurrency() int {
	if ch := aiSemaphore.Load(); ch != nil {
		return cap(*ch)
	}
	return 0
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
	// FitInside 把 Width/Height 解释成「框」而不是「画布」：等比缩到框内，不拉伸也不放大。
	// 阅读器的适应模式要的就是这个语义；封面与缩略图不设它，仍按画布精确缩放。
	FitInside bool
}

// 图片解码内存保护阈值：超过硬上限视为解码炸弹直接拒绝，超过告警阈值仅记录。
const (
	maxDecodePixels      = 100_000_000 // 约 10000x10000，超出视为不可安全处理
	largeImageWarnPixels = 25_000_000  // 约 5000x5000，记录告警但仍处理

	// MaxTargetDimension 是单边输出尺寸上限。解码侧的 maxDecodePixels 只约束「源图有多大」，
	// 管不住「要输出多大」——目标画布由调用方的 Width/Height 决定，不设限时一次请求即可要求
	// 分配数 GB 的像素缓冲。8192 覆盖任何真实的漫画页需求（4K 屏也只要 2160）。
	MaxTargetDimension = 8192
	// maxTargetPixels 额外约束目标面积：8192x8192 已是 268M 像素 / 约 1GiB RGBA，
	// 单边合法但组合起来仍可能打爆内存，故再加一道面积闸。
	maxTargetPixels = 40_000_000 // 约 8192x4882 或 6000x6666
)

// ValidateTargetDimensions 校验调用方请求的输出尺寸是否在安全范围内。
// 负值、超过单边上限、或面积超预算都直接拒绝，避免进入 resize 后才 OOM。
// 供 HTTP 层在解析查询参数时提前拦截（返回 400），也作为 ProcessImage 内部的第二道防线。
func ValidateTargetDimensions(width, height int) error {
	if width < 0 || height < 0 {
		return fmt.Errorf("negative target dimension: %dx%d", width, height)
	}
	if width > MaxTargetDimension || height > MaxTargetDimension {
		return fmt.Errorf("target dimension exceeds limit %d: %dx%d", MaxTargetDimension, width, height)
	}
	if area := int64(width) * int64(height); area > maxTargetPixels {
		return fmt.Errorf("target area too large: %dx%d (%d pixels)", width, height, area)
	}
	return nil
}

// 阅读器页图的目标尺寸档位。尺寸由客户端按容器算出，逐像素照单全收会让同一页在每个窗口
// 宽度上各留一份缓存；向上取整到步长的倍数后，一本书只会落在有限几档上。
const (
	// ReaderSizeStep 是档位步长。图略大于显示尺寸，浏览器再做一次小幅缩小，画质损失可忽略——
	// 决定画质的是「原图 → 档位」这一大步。
	ReaderSizeStep = 256
	// MaxReaderDimension 是阅读器单边档位上限，比 MaxTargetDimension 严得多：超出显示需要的
	// 部分只是白付编码与带宽，4K 屏叠上 DPR 也不该把一页拉成巨图。
	MaxReaderDimension = 3072
)

// SnapTargetDimension 把目标尺寸向上取整到 ReaderSizeStep 的倍数并夹到 MaxReaderDimension。
// 前端已经按同一套档位发请求，服务端仍要再夹一次——尺寸是外部输入，不能只信前端。
func SnapTargetDimension(value int) int {
	if value <= 0 {
		return 0
	}
	if value >= MaxReaderDimension {
		return MaxReaderDimension
	}
	return ((value + ReaderSizeStep - 1) / ReaderSizeStep) * ReaderSizeStep
}

// normalizeWaifu2xFormat 把用户可控的输出格式收敛到白名单。
// 该值会成为沙盒内输出文件的扩展名并作为 -f 参数传给引擎，任何未知值一律回落 webp。
func normalizeWaifu2xFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		return "png"
	case "jpg", "jpeg":
		return "jpg"
	default:
		return "webp"
	}
}

// normalizeWaifu2xScale 把放大倍数收敛到引擎实际支持的档位。
// 未钳制时 w2x_scale=99999 会让引擎去生成一张天文尺寸的图。
func normalizeWaifu2xScale(scale int) int {
	switch scale {
	case 1, 2, 4:
		return scale
	default:
		return 2
	}
}

// normalizeWaifu2xNoise 把降噪等级夹到引擎支持的 [-1, 3]。
func normalizeWaifu2xNoise(noise int) int {
	if noise < -1 {
		return -1
	}
	if noise > 3 {
		return 3
	}
	return noise
}

// formatMatchesContentType 判断目标输出格式是否与源 Content-Type 一致（jpg 归一化为 jpeg）。
func formatMatchesContentType(format, contentType string) bool {
	f := strings.ToLower(strings.TrimSpace(format))
	if f == "jpg" {
		f = "jpeg"
	}
	return f != "" && strings.Contains(strings.ToLower(contentType), f)
}

// FilterChangesPixels 判断滤镜在给定目标尺寸下是否真会改动像素。
//
// 除 AI 放大（waifu2x/realcugan/ncnn）外，滤镜只是重采样用的插值核：目标尺寸为空时
// resize.Resize 与 imaging.Fit 都原样返回输入图，为它解一次码再编一次码是白付 CPU，
// 还丢掉了原始字节透传。HTTP 层用同一判据决定请求是否产出派生图（是否值得进缓存）。
func FilterChangesPixels(filter string, width, height int) bool {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "":
		return false
	case "waifu2x", "realcugan", "ncnn":
		return true
	default:
		return width > 0 || height > 0
	}
}

func ProcessImage(data []byte, contentType string, opts ProcessOptions) ([]byte, string, error) {
	// 目标尺寸闸门（第二道防线，HTTP 层已先校验一次）：负值经 uint() 转换会变成天文数字，
	// 超大值会让 resize 直接申请数 GB 缓冲。必须在解码之前拒绝，否则白白解一次图。
	if err := ValidateTargetDimensions(opts.Width, opts.Height); err != nil {
		return nil, "", fmt.Errorf("invalid target size: %w", err)
	}

	// AI 放大参数白名单。这三个值来自 HTTP 查询串（w2x_scale / w2x_noise / w2x_format），
	// 下游会把 Waifu2xFormat 拼进沙盒输出路径、子进程 argv，以及回传的 Content-Type。
	// 旧代码直接 filepath.Join(sandboxDir, "out."+format)，一个 "../../../tmp/x" 就能让
	// 引擎把文件写到沙盒外。必须在入口归一化——放在 execWaifu2x 内部不行，那里拿的是
	// opts 的值拷贝，归一化结果传不回这里的 Content-Type 推导。
	opts.Waifu2xFormat = normalizeWaifu2xFormat(opts.Waifu2xFormat)
	opts.Waifu2xScale = normalizeWaifu2xScale(opts.Waifu2xScale)
	opts.Waifu2xNoise = normalizeWaifu2xNoise(opts.Waifu2xNoise)

	// 如果没有任何缩放/滤镜/质量/裁切需求，且目标格式未指定或与源格式一致，直接透传原始字节，
	// 避免「源已是目标格式（如 format=webp 而源就是 webp）」仍白白解码 + 重编码一次（且可能损质）。
	// 只给滤镜不给尺寸也算「无需求」——见 FilterChangesPixels。
	if opts.Width == 0 && opts.Height == 0 && !FilterChangesPixels(opts.Filter, opts.Width, opts.Height) && opts.Quality == 0 && !opts.AutoCrop {
		if opts.Format == "" || formatMatchesContentType(opts.Format, contentType) {
			return data, contentType, nil
		}
	}

	// 预检图片尺寸而不完全解码，据此拦截解码炸弹：小体积压缩文件可声明极大画布，
	// 完全解码会瞬间耗尽内存。用 int64 计算面积避免超大尺寸相乘时溢出。
	readerConfig := bytes.NewReader(data)
	if config, _, err := image.DecodeConfig(readerConfig); err == nil {
		area := int64(config.Width) * int64(config.Height)
		if area > maxDecodePixels {
			return nil, "", fmt.Errorf("image too large to process safely: %dx%d (%d pixels)", config.Width, config.Height, area)
		}
		if area > largeImageWarnPixels {
			// 大图（如 5000x5000+）在小型服务器上解码开销较高，记录以便排障，但仍尝试处理。
			slog.Warn("Large image detected", "width", config.Width, "height", config.Height, "area", area)
		}
	}

	img, _, err := decodeImage(data, contentType)
	if err != nil {
		return nil, "", fmt.Errorf("decode image err: %w", err)
	}

	var newImg = img

	// 自动裁切白边逻辑
	if opts.AutoCrop {
		newImg = autoCropImage(newImg)
		// 重要：裁切后的 SubImage 可能带有非零的 Min.X/Y 和原始父图的步长(Stride)
		// 这会导致某些编码器(如 cgo 封装的库)出现偏移、斜切或花屏
		// 必须执行归一化，将其绘制到一个全新的从 (0,0) 开始的干净画布中
		newImg = flattenImage(newImg)
	}

	// 目标尺寸只认调用方给的值，为空时不得补成源尺寸：同尺寸重采样在 resize 与 imaging
	// 里都是恒等操作，补上去只是让重采样空转一趟，白付一次解码与重编码。
	targetWidth := uint(opts.Width)
	targetHeight := uint(opts.Height)

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

	// 软件缩放 + 编码是纯 CPU 工作，用信号量把并发封顶到核数，避免阅读器预取/多用户并发时 CPU 过载抖动。
	// AI 路径（execWaifu2x）已在上方用 aiSemaphore 门控并在成功时提前返回，不会走到这里；AI 回退时
	// execWaifu2x 已释放 aiSemaphore 再进入此处，故不存在与 aiSemaphore 的双占。channel 快照进局部
	// 变量，acquire 与 release 用同一引用，避免热更新替换全局指针导致令牌错配。
	if swPtr := softwareSemaphore.Load(); swPtr != nil {
		sw := *swPtr
		sw <- struct{}{}
		defer func() { <-sw }()
	}

	if targetWidth > 0 || targetHeight > 0 {
		boxWidth, boxHeight := fitBox(newImg.Bounds(), int(targetWidth), int(targetHeight))
		switch opts.Filter {
		case "bspline":
			newImg = imaging.Fit(newImg, boxWidth, boxHeight, imaging.BSpline)
		case "catmullrom":
			newImg = imaging.Fit(newImg, boxWidth, boxHeight, imaging.CatmullRom)
		default:
			var interp = resize.Bilinear
			switch opts.Filter {
			case "mitchell":
				interp = resize.MitchellNetravali
			case "lanczos2":
				interp = resize.Lanczos2
			case "bicubic":
				interp = resize.Bicubic
			case "lanczos3":
				interp = resize.Lanczos3
			case "nearest":
				interp = resize.NearestNeighbor
			}
			if opts.FitInside {
				newImg = resize.Thumbnail(uint(boxWidth), uint(boxHeight), newImg, interp)
			} else {
				newImg = resize.Resize(targetWidth, targetHeight, newImg, interp)
			}
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
		quality := opts.Quality
		if quality <= 0 {
			quality = 85 // 默认质量
		}
		newContentType, err = encodeWebP(&buf, newImg, quality, false)
	case "avif":
		err = avif.Encode(&buf, newImg, avifDeliveryOptions(opts.Quality))
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

// avifEncodeSpeed 是 avif 编码档位：0 最慢、体积最小，10 最快、体积最大。必须显式赋值——
// 库对 Quality 判 <=0 才回落默认，对 Speed 只判 <0，零值 0 会被当作「显式选了最慢档」。
// 取 8 是耗时/体积曲线的拐点：从 10 降到 8，400px 封面体积省掉两成，代价是编码慢一个小几倍、
// 仍在毫秒级；再往下降到 7 及以下，耗时逐档翻倍而体积只再省个位数百分比。缩略图每本书一张、
// 批量生成，速度权重高于最后那点体积。
const avifEncodeSpeed = 8

// avifDeliveryOptions 组装直接交付客户端的 avif 编码参数（封面缩略图与阅读页图）。
// 色度取 4:2:0：库默认值，同档位下比零值的 4:4:4 又小又快。
func avifDeliveryOptions(quality int) avif.Options {
	return avif.Options{
		Quality:           quality,
		Speed:             avifEncodeSpeed,
		ChromaSubsampling: image.YCbCrSubsampleRatio420,
	}
}

// avifIntermediateOptions 组装 AI 放大沙盒里中间态落盘的 avif 编码参数。这张图只喂给外部引擎、
// 不外发，取满质量与 4:4:4 色度保住细节，不做交付路径的体积取舍。
func avifIntermediateOptions() avif.Options {
	return avif.Options{
		Quality:           100,
		Speed:             avifEncodeSpeed,
		ChromaSubsampling: image.YCbCrSubsampleRatio444,
	}
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
	// 如果由于读页并发过高，此处会阻塞协程直到前序 AI 任务完成。
	// 把 channel 快照进 sem，确保 acquire 与 release 用的是同一个 channel，
	// 即使中途 InitProcessor 替换了全局引用也不会令牌错配。
	if semPtr := aiSemaphore.Load(); semPtr != nil {
		sem := *semPtr
		sem <- struct{}{}
		defer func() { <-sem }() // 放锁 (Semaphore Release)
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

	// 自定义引擎路径加固：仅接受“绝对路径 + 存在 + 常规文件”。拒绝相对路径（可能随 cwd 解析到意外
	// 可执行文件）和指向目录的路径，降低“改配置即执行任意本地文件”链的滥用面；不满足则退回全局嗅探。
	// 该端点的写入侧应配合 server.auth 鉴权（见 controller.requireAuth）。
	if customPath != "" {
		switch info, err := os.Stat(customPath); {
		case !filepath.IsAbs(customPath):
			slog.Warn("Ignoring non-absolute custom engine path (security hardening)", "custom_path", customPath)
		case err != nil:
			slog.Warn("Custom engine path specified but not accessible", "custom_path", customPath, "error", err)
		case info.IsDir():
			slog.Warn("Ignoring custom engine path pointing to a directory", "custom_path", customPath)
		default:
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
		inExt = webpIntermediateExtension()
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
			_, err = encodeWebP(f, img, 100, true)
		} else if strings.Contains(contentType, "png") {
			err = png.Encode(f, img)
		} else if strings.Contains(contentType, "avif") {
			err = avif.Encode(f, img, avifIntermediateOptions())
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

// fitBox 把只给了一条边的目标尺寸补成一个完整的框，另一条边按源图纵横比推出。
//
// imaging.Fit 对任一边 <= 0 直接返回空图——阅读器的「适应宽度/高度」只给一条边，
// 补不出框时 bspline 与 catmullrom 交出的是整页空白。补齐后 Fit 缩出的结果与
// 「等比缩到该边」等价，两个滤镜与 resize 那一支的语义因此对齐。
//
// 推出来的那条边额外放宽 1 像素：Fit 按纵横比挑约束边，框恰好同比例时取整误差会让
// 调用方给的那条边少 1 像素；放宽后约束边一定是给定的那条，输出尺寸精确等于请求值。
func fitBox(bounds image.Rectangle, width, height int) (int, int) {
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 || (width > 0 && height > 0) {
		return width, height
	}
	if width > 0 {
		return width, int(math.Ceil(float64(width)*float64(srcH)/float64(srcW))) + 1
	}
	if height > 0 {
		return int(math.Ceil(float64(height)*float64(srcW)/float64(srcH))) + 1, height
	}
	return width, height
}

// flattenImage 把图像归一化成一张起点在 (0,0)、行间无空隙的新画布，消除编码器对 Bounds.Min
// 与 Stride 的兼容性问题（防花屏）。
//
// 裁切产生的 SubImage 会继承**父图**的 Stride，而按 Pix 整段直读的编码器（avif 对
// *image.RGBA 就是这么走的）按「Stride 等于一行的字节数」解释缓冲，逐行错位读出整页花屏。
// 判据因此必须同时看起点与步长：裁切框左上角恰好落在 (0,0) 时起点是干净的，步长却不是。
func flattenImage(img image.Image) image.Image {
	if img == nil {
		return nil
	}

	bounds := img.Bounds()
	// 起点在原点且行间紧凑的图不必重绘。没发生裁切时这是常态，整图拷贝白付。
	if bounds.Min.X == 0 && bounds.Min.Y == 0 && hasCompactRows(img) {
		return img
	}

	// 画布类型按源选：非预乘的源留在 NRGBA，免掉 alpha 预乘往返的精度损失；其余一律 RGBA——
	// image/draw 只对 *image.RGBA 目标备了 YCbCr 与 RGBA 的整行快路径，换成 NRGBA 会掉进
	// 逐像素装箱的通用路径，一张漫画页就是几百万次分配。
	rect := image.Rect(0, 0, bounds.Dx(), bounds.Dy())
	var canvas draw.Image
	switch img.(type) {
	case *image.NRGBA, *image.NRGBA64:
		canvas = image.NewNRGBA(rect)
	default:
		canvas = image.NewRGBA(rect)
	}
	draw.Draw(canvas, canvas.Bounds(), img, bounds.Min, draw.Src)
	return canvas
}

// hasCompactRows 判断像素缓冲是否按行紧凑排布，即 Stride 恰好等于一行的字节数。
//
// 只认标准库里带 Pix/Stride 的具体类型——编码器的「按 Pix 整段直读」快路径也只认这些，
// 其它实现（含自定义 image.Image）只会被逐像素读取，一律当作已归一化。
// YCbCr 系只看 YStride：起点已在原点时 YStride 紧凑等价于子图宽度就是父图宽度，
// 色度平面的步长随之也是紧凑的。
func hasCompactRows(img image.Image) bool {
	switch m := img.(type) {
	case *image.RGBA:
		return m.Stride == 4*m.Rect.Dx()
	case *image.NRGBA:
		return m.Stride == 4*m.Rect.Dx()
	case *image.RGBA64:
		return m.Stride == 8*m.Rect.Dx()
	case *image.NRGBA64:
		return m.Stride == 8*m.Rect.Dx()
	case *image.Gray:
		return m.Stride == m.Rect.Dx()
	case *image.Gray16:
		return m.Stride == 2*m.Rect.Dx()
	case *image.Alpha:
		return m.Stride == m.Rect.Dx()
	case *image.Alpha16:
		return m.Stride == 2*m.Rect.Dx()
	case *image.CMYK:
		return m.Stride == 4*m.Rect.Dx()
	case *image.Paletted:
		return m.Stride == m.Rect.Dx()
	case *image.YCbCr:
		return m.YStride == m.Rect.Dx()
	case *image.NYCbCrA:
		return m.YStride == m.Rect.Dx() && m.AStride == m.Rect.Dx()
	default:
		return true
	}
}
