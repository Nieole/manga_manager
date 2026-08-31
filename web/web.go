// Package web 把前端构建产物目录 dist 以 embed.FS 编译进二进制，使服务端自带整套前端资源。
//
// 边界：本包只导出 FS 这一个只读文件系统，没有其他 Go 逻辑。静态路由挂载、SPA 回退到
// dist/index.html，以及 Content-Type、ETag、Cache-Control 的写入都由 cmd/server 决定
// （writeStaticContent）。资料库、系列、书、任务等业务接口与 OPDS、KOReader 同步端点属于
// internal/api，书页与封面的字节流也由那里产出，不经过本 FS。dist 由 web 下的前端源码构建
// 产生，不进版本库；dist 缺失时本包编译不过。
package web

import "embed"

//go:embed all:dist
var FS embed.FS
