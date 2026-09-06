// Package scanner 把资料库目录树变成数据库里的系列与书：发现归档文件、派生系列归属、写入书记录、生成封面缩略图。
//
// 属于它的是扫描本身的决策：ScanProfile 定每本书读到多深，mtime+size 做增量跳过，renameIndex 把改名的文件重连回原书记录，
// 归档内嵌的 ComicInfo 写进系列，被触及系列的统计随扫描刷新；CleanupLibrary 清除文件已消失的系列与书，CleanupThumbnails 清除无人引用的缩略图；
// FileWatcher 去抖后自动触发库扫描与 CleanupLibrary，两者都经装配期交进来的出口（WatcherHooks）走，本包不判「这个库是不是已经在扫」。
//
// 边界：归档解析归 parser、缩略图编码归 images、指纹归 koreader；存储令牌向 storageio 申请，暂停闸门用 taskcontrol.Wait。
// 任务键、作用域与运行状态由 api 编排，「同一件事不会同时跑两遍」由那一处的准入回答（ADR 0005）；
// 本包只经**扫描观察者**上报扫描的进度与指标、经 CoverObserver 上报每库那一批封面的推进，另记一笔「哪些库此刻在扫」供监听器判断敢不敢清理；
// 外部刮削归 metadata、提案裁决归 api，外部库归 external 且不参与扫描。
package scanner
