// Package parser 是归档解析层：按路径打开 cbz/zip 与 cbr/rar，列出其中可读的图片页并定出页序
// （自然序，封面关键字与浅层目录优先），按条目名取原始页字节（解压体积有硬上限，拦解压炸弹），
// 解析与序列化 ComicInfo.xml。写回只支持 zip/cbz，rar/cbr 返回 ErrArchiveNotWritable；全局归档池
// 按路径发放共享句柄，被淘汰的句柄等引用归零后才真正关闭。
//
// 边界：本包只认文件路径，不认资料库/系列/书，也不碰数据库——目录树遍历、归属推导与落库属于
// scanner；页字节的解码缩放编码属于 images；动磁盘前的存储令牌属于 storageio，由调用方申请；
// 系列内卷话号排序属于 booksort，本包只定归档内的页序。本包不 import 任何兄弟包。
package parser
