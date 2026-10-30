// Package config 定义 config.yaml 的结构，负责读取、归一化、校验（纯值域与触盘两档）与热重载，
// 并以 Manager 提供线程安全的配置快照，供后端各包共享同一份运行时配置；它不 import 任何兄弟包。
//
// 归它的还有：文件缺失时生成默认配置、配置文件的原子写入与权限位、密钥掩码（MaskSecrets /
// RestoreMaskedSecrets）、按路径解析存储策略（ResolveStoragePolicy），以及扫描等级、归档格式、
// 存储介质档位这些取值词汇的判定与归一化。不归它的：配置派生的资源由 runtimecfg.Resources
// 发布及退休，存储令牌排队在 storageio；设置发布由 application/configuration 编排，HTTP 端点在 api。
// 资料库逐库设置（扫描间隔、scan_formats）存在 database.Library。
package config
