// Package storageio 发放存储令牌：要动磁盘的调用方先在此按卷排队取得许可，阅读请求优先于后台作业。
//
// 边界：本包只管许可与排队——按卷键加并发上限计数（Scheduler.Acquire，应用各处共用包级实例 Default）、
// 让后台作业为阅读让路（WorkKindReader 与 PauseWhenReading/IdleOnly）、整体暂停与恢复后台 IO、
// 给出观测快照。它不碰磁盘，也不认识资料库、系列与书：卷键由 config 推导（VolumeKey、
// ResolveStoragePolicy），传入的并发上限由调用方从存储策略挑选，何时取令牌、取哪一类也由调用方决定
// （scanner 与 api）；诊断与暂停的 HTTP 端点属于 api；单个任务的暂停闸门是 taskcontrol.PauseGate，
// 与这里的后台 IO 暂停是两件事。
package storageio
