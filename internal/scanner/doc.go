// Package scanner 把资料库目录树变成数据库里的系列与书：发现归档文件、派生系列归属、写入书记录、生成封面缩略图。
//
// 属于它的是扫描本身的决策：ScanProfile 定每本书读到多深，mtime+size 做增量跳过，renameIndex 把改名的文件重连回原书记录，
// 归档内嵌的 ComicInfo 写进系列，被触及系列的统计随扫描刷新；CleanupLibrary 清除文件已消失的系列与书，CleanupThumbnails 清除无人引用的缩略图；
// FileWatcher 去抖后自动触发库扫描与 CleanupLibrary，同一资料库或系列上的并发扫描由 ErrScanAlreadyRunning 拒绝。
//
// 边界：归档解析归 parser、缩略图编码归 images、指纹归 koreader；存储令牌向 storageio 申请，暂停闸门用 taskcontrol.Wait。
// 任务键、作用域与任务状态由 api 编排，本包只经回调上报进度与指标；外部刮削归 metadata、提案裁决归 api，外部库归 external 且不参与扫描。
package scanner
