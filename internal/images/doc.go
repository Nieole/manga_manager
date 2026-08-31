// Package images 是图片转码单元：把一段字节按尺寸、滤镜、格式与质量解码、裁边、缩放并重编码为
// jpeg/png/webp/avif；无处理需求且目标格式已匹配时原样透传。ValidateTargetDimensions 与解码前的
// 像素预检挡住超大目标画布与解码炸弹；滤镜取 waifu2x/realcugan/ncnn 时由 execWaifu2x 在临时沙盒
// 里调外部引擎子进程，失败回退 lanczos3。InitProcessor 按配置重建 AI 并发闸，软件闸取 CPU 核数。
//
// 边界：读字节、缓存、ETag 与 HTTP 都不属于它——scanner.generateBookThumbnail 与 api 的
// servePageImageByNumber 先取存储令牌、从归档读出页字节再交进来；配置经 runtimecfg.Apply 转入。
// 除 AI 沙盒的临时文件外本包不碰文件系统，不导入兄弟包，也不认识资料库、系列与书。
package images
