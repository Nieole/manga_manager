# 全项目架构整理实施队列

阶段：拆分方案已由用户确认，工单已发布，尚未实施。所有工单的 ready-for-agent 仅表示就绪，完成状态与运行证据由后续实施阶段维护。

需求依据：[父规格](spec.md)。设计依据：[已确认设计](design.md)。事实范围：[审查线索](audit.md)。本索引不重复逐票验收条件。

## 执行约束

- 按阻塞关系选择前沿，编号不是固定串行排期；共用文件或合并冲突需要协调，不等同增加硬依赖。
- 每票包含实际生产消费者及适用行为验证；没有变化的 schema 或 UI 不为形式额外修改。
- 01–07 采用读者模型扩展、功能/协议分批迁移与收缩；42–47 采用宿主扩展、按依赖面迁移与收缩。各批保持可构建和适用门禁通过。
- 22 提供空闲删除与占用拒绝的可用前置切片，23 完成主动取消、排空及补偿；最终删除行为依父规格和 23 验收。
- 相应前置整理先于依赖票；兼容入口只在仍有调用时保留，收缩票负责核对退出。测试支持文件中的合理故障 seam 不等同生产规则副本。
- 48 负责联合验收与证据，不兜底实现前票遗漏功能。每条故事保留实际实现、消费者、执行结果与结构判断的映射。

## 工单索引

| 编号 | 工单 | 阻塞 | 类型 |
| --- | --- | --- | --- |
| 01 | [单书读者视图与导航](issues/01-reader-book-view.md) | 无 | 纵向切片 |
| 02 | [系列列表的主体筛选与分页](issues/02-catalog-reader-pagination.md) | 01 | 纵向切片 |
| 03 | [藏书编排的读者查询](issues/03-organized-reader-queries.md) | 02 | 纵向切片 |
| 04 | [OPDS 与 Mihon 的读者视图](issues/04-protocol-reader-queries.md) | 02 | 纵向切片 |
| 05 | [阅读位置与标记的窄事务](issues/05-reading-write-transactions.md) | 01 | 纵向切片 |
| 06 | [书签与时长的完整命令](issues/06-reader-time-bookmarks.md) | 01 | 纵向切片 |
| 07 | [退出旧读者模型兼容面](issues/07-reader-model-contract.md) | 03、04、05、06 | 广泛迁移收缩 |
| 08 | [人工元数据保存与编辑草稿](issues/08-manual-metadata-usecase.md) | 无 | 纵向切片 |
| 09 | [批量元数据编辑的提交结果](issues/09-bulk-metadata-usecase.md) | 08 | 纵向切片 |
| 10 | [提案部分应用与终态竞争](issues/10-proposal-concurrency.md) | 无 | 纵向切片 |
| 11 | [人工合集与智能快照完整用例](issues/11-collections-snapshot.md) | 无 | 纵向切片 |
| 12 | [AI 分组的权威裁决结果](issues/12-ai-review-authority.md) | 11 | 纵向切片 |
| 13 | [关系编辑与作品群持久修复](issues/13-franchise-durable-repair.md) | 无 | 纵向切片 |
| 14 | [级联变化与作品群修复覆盖](issues/14-franchise-cascade-facts.md) | 13 | 纵向切片 |
| 15 | [阅读清单顺序与窄写用例](issues/15-ordered-reading-list-writes.md) | 03 | 纵向切片 |
| 16 | [页读取的共享内容能力](issues/16-book-content-pages.md) | 无 | 纵向切片 |
| 17 | [整书下载与读取句柄](issues/17-book-download-handles.md) | 16 | 纵向切片 |
| 18 | [ComicInfo 回写的安全发布](issues/18-comicinfo-publication.md) | 17 | 纵向切片 |
| 19 | [外部库全资料库边界](issues/19-external-root-boundary.md) | 无 | 纵向切片 |
| 20 | [复制中的取消与提交结果](issues/20-transfer-cancellation.md) | 无 | 纵向切片 |
| 21 | [块边界暂停与安全续拷](issues/21-transfer-pause-resume.md) | 20 | 纵向切片 |
| 22 | [空闲删库与占用拒绝](issues/22-library-delete-reservation.md) | 无 | 纵向切片 |
| 23 | [删库取消排空与失败补偿](issues/23-library-delete-drain.md) | 22 | 纵向切片 |
| 24 | [资料库创建修改与导航功能](issues/24-library-create-update.md) | 22 | 纵向切片 |
| 25 | [迁移持久过程与恢复分工](issues/25-relocation-process-storage.md) | 无 | 纵向切片 |
| 26 | [书身份与设备匹配能力](issues/26-book-identity-matching.md) | 无 | 纵向切片 |
| 27 | [扫描摄入的完整内部用例](issues/27-scan-ingestion.md) | 26 | 纵向切片 |
| 28 | [文件监听与安全清理协调](issues/28-watch-ingestion-coordination.md) | 27 | 纵向切片 |
| 29 | [元数据与 AI 消费能力](issues/29-metadata-consumer-ports.md) | 无 | 纵向切片 |
| 30 | [配置发布与生效范围](issues/30-configuration-effects.md) | 无 | 纵向切片 |
| 31 | [资源热切换与关闭所有权](issues/31-runtime-resource-ownership.md) | 16、30 | 纵向切片 |
| 32 | [设置分区编辑会话](issues/32-settings-edit-session.md) | 30 | 纵向切片 |
| 33 | [离线归属与补传功能](issues/33-offline-feature.md) | 无 | 纵向切片 |
| 34 | [查询与已提交操作影响分离](issues/34-catalog-query-action-effects.md) | 05、09 | 纵向切片 |
| 35 | [看板与统计的失败陈旧呈现](issues/35-query-failure-ui.md) | 无 | 纵向切片 |
| 36 | [任务观测功能与传输契约归属](issues/36-task-observation-contracts.md) | 无 | 纵向切片 |
| 37 | [重启声明与有损合并事件](issues/37-run-declarations.md) | 无 | 纵向切片 |
| 38 | [站点初始化与账户用例入口](issues/38-account-initialization.md) | 无 | 纵向切片 |
| 39 | [账户管理与最后管理员保护](issues/39-account-administration.md) | 38 | 纵向切片 |
| 40 | [改密与会话资格交接](issues/40-credentials-sessions.md) | 38 | 纵向切片 |
| 41 | [目录组合查询与评价用例](issues/41-composite-queries-cache.md) | 02、29 | 纵向切片 |
| 42 | [独立装配根与新宿主扩展](issues/42-bootstrap-expand.md) | 24、31、36、38、41 | 广泛宿主迁移扩展 |
| 43 | [阅读与目录 HTTP 依赖迁移](issues/43-http-reader-query-migration.md) | 07、42 | 广泛宿主迁移批次 |
| 44 | [元数据与藏书编排 HTTP 迁移](issues/44-http-organization-migration.md) | 09、10、12、14、15、42 | 广泛宿主迁移批次 |
| 45 | [资料库与运行磁盘 HTTP 迁移](issues/45-http-library-runs-migration.md) | 18、19、21、23、25、28、37、42 | 广泛宿主迁移批次 |
| 46 | [配置账户与管理适配迁移](issues/46-http-config-accounts-migration.md) | 32、39、40、42 | 广泛宿主迁移批次 |
| 47 | [删除旧宿主与全量依赖兼容面](issues/47-bootstrap-contract.md) | 43、44、45、46 | 广泛宿主迁移收缩 |
| 48 | [全项目联合验收与归属证据](issues/48-integrated-acceptance.md) | 33、34、35、47 | 纵向切片 |

## 初始前沿

- [01: 单书读者视图与导航](issues/01-reader-book-view.md)
- [08: 人工元数据保存与编辑草稿](issues/08-manual-metadata-usecase.md)
- [10: 提案部分应用与终态竞争](issues/10-proposal-concurrency.md)
- [11: 人工合集与智能快照完整用例](issues/11-collections-snapshot.md)
- [13: 关系编辑与作品群持久修复](issues/13-franchise-durable-repair.md)
- [16: 页读取的共享内容能力](issues/16-book-content-pages.md)
- [19: 外部库全资料库边界](issues/19-external-root-boundary.md)
- [20: 复制中的取消与提交结果](issues/20-transfer-cancellation.md)
- [22: 空闲删库与占用拒绝](issues/22-library-delete-reservation.md)
- [25: 迁移持久过程与恢复分工](issues/25-relocation-process-storage.md)
- [26: 书身份与设备匹配能力](issues/26-book-identity-matching.md)
- [29: 元数据与 AI 消费能力](issues/29-metadata-consumer-ports.md)
- [30: 配置发布与生效范围](issues/30-configuration-effects.md)
- [33: 离线归属与补传功能](issues/33-offline-feature.md)
- [35: 看板与统计的失败陈旧呈现](issues/35-query-failure-ui.md)
- [36: 任务观测功能与传输契约归属](issues/36-task-observation-contracts.md)
- [37: 重启声明与有损合并事件](issues/37-run-declarations.md)
- [38: 站点初始化与账户用例入口](issues/38-account-initialization.md)

此处仅列发布时可立即领取的工单；实施阶段须结合各票实际完成状态重算前沿。

## 规格覆盖索引

| 规格故事 | 承接工单 |
| --- | --- |
| US 1 | [47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 2 | [47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 3 | [47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 4 | [43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[46](issues/46-http-config-accounts-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 5 | [43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[46](issues/46-http-config-accounts-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 6 | [43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 7 | [43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 8 | [42](issues/42-bootstrap-expand.md)、[43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[46](issues/46-http-config-accounts-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 9 | [42](issues/42-bootstrap-expand.md)、[43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[46](issues/46-http-config-accounts-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 10 | [41](issues/41-composite-queries-cache.md)、[43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 11 | [47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 12 | [47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 13 | [07](issues/07-reader-model-contract.md)、[43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[46](issues/46-http-config-accounts-migration.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 14 | [47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 15 | [01](issues/01-reader-book-view.md) |
| US 16 | [01](issues/01-reader-book-view.md) |
| US 17 | [01](issues/01-reader-book-view.md)、[07](issues/07-reader-model-contract.md) |
| US 18 | [01](issues/01-reader-book-view.md)、[03](issues/03-organized-reader-queries.md)、[04](issues/04-protocol-reader-queries.md)、[41](issues/41-composite-queries-cache.md)、[43](issues/43-http-reader-query-migration.md) |
| US 19 | [01](issues/01-reader-book-view.md)、[04](issues/04-protocol-reader-queries.md) |
| US 20 | [01](issues/01-reader-book-view.md) |
| US 21 | [02](issues/02-catalog-reader-pagination.md)、[03](issues/03-organized-reader-queries.md)、[41](issues/41-composite-queries-cache.md) |
| US 22 | [02](issues/02-catalog-reader-pagination.md)、[03](issues/03-organized-reader-queries.md)、[41](issues/41-composite-queries-cache.md) |
| US 23 | [01](issues/01-reader-book-view.md)、[02](issues/02-catalog-reader-pagination.md)、[04](issues/04-protocol-reader-queries.md) |
| US 24 | [01](issues/01-reader-book-view.md)、[06](issues/06-reader-time-bookmarks.md) |
| US 25 | [01](issues/01-reader-book-view.md)、[05](issues/05-reading-write-transactions.md) |
| US 26 | [01](issues/01-reader-book-view.md)、[05](issues/05-reading-write-transactions.md) |
| US 27 | [01](issues/01-reader-book-view.md)、[03](issues/03-organized-reader-queries.md)、[15](issues/15-ordered-reading-list-writes.md) |
| US 28 | [01](issues/01-reader-book-view.md)、[02](issues/02-catalog-reader-pagination.md)、[03](issues/03-organized-reader-queries.md)、[04](issues/04-protocol-reader-queries.md)、[43](issues/43-http-reader-query-migration.md) |
| US 29 | [01](issues/01-reader-book-view.md)、[02](issues/02-catalog-reader-pagination.md)、[41](issues/41-composite-queries-cache.md) |
| US 30 | [01](issues/01-reader-book-view.md)、[04](issues/04-protocol-reader-queries.md)、[06](issues/06-reader-time-bookmarks.md)、[43](issues/43-http-reader-query-migration.md) |
| US 31 | [05](issues/05-reading-write-transactions.md)、[06](issues/06-reader-time-bookmarks.md)、[07](issues/07-reader-model-contract.md)、[11](issues/11-collections-snapshot.md)、[15](issues/15-ordered-reading-list-writes.md) |
| US 32 | [05](issues/05-reading-write-transactions.md)、[11](issues/11-collections-snapshot.md) |
| US 33 | [08](issues/08-manual-metadata-usecase.md)、[44](issues/44-http-organization-migration.md) |
| US 34 | [08](issues/08-manual-metadata-usecase.md) |
| US 35 | [09](issues/09-bulk-metadata-usecase.md) |
| US 36 | [27](issues/27-scan-ingestion.md) |
| US 37 | [10](issues/10-proposal-concurrency.md) |
| US 38 | [08](issues/08-manual-metadata-usecase.md)、[09](issues/09-bulk-metadata-usecase.md)、[10](issues/10-proposal-concurrency.md)、[27](issues/27-scan-ingestion.md)、[44](issues/44-http-organization-migration.md) |
| US 39 | [05](issues/05-reading-write-transactions.md)、[06](issues/06-reader-time-bookmarks.md)、[08](issues/08-manual-metadata-usecase.md)、[11](issues/11-collections-snapshot.md)、[15](issues/15-ordered-reading-list-writes.md) |
| US 40 | [02](issues/02-catalog-reader-pagination.md)、[08](issues/08-manual-metadata-usecase.md)、[41](issues/41-composite-queries-cache.md) |
| US 41 | [05](issues/05-reading-write-transactions.md)、[08](issues/08-manual-metadata-usecase.md)、[09](issues/09-bulk-metadata-usecase.md)、[18](issues/18-comicinfo-publication.md) |
| US 42 | [05](issues/05-reading-write-transactions.md)、[09](issues/09-bulk-metadata-usecase.md) |
| US 43 | [05](issues/05-reading-write-transactions.md)、[06](issues/06-reader-time-bookmarks.md) |
| US 44 | [05](issues/05-reading-write-transactions.md)、[07](issues/07-reader-model-contract.md)、[47](issues/47-bootstrap-contract.md) |
| US 45 | [10](issues/10-proposal-concurrency.md) |
| US 46 | [10](issues/10-proposal-concurrency.md) |
| US 47 | [10](issues/10-proposal-concurrency.md) |
| US 48 | [10](issues/10-proposal-concurrency.md) |
| US 49 | [10](issues/10-proposal-concurrency.md) |
| US 50 | [10](issues/10-proposal-concurrency.md) |
| US 51 | [10](issues/10-proposal-concurrency.md) |
| US 52 | [10](issues/10-proposal-concurrency.md) |
| US 53 | [10](issues/10-proposal-concurrency.md) |
| US 54 | [10](issues/10-proposal-concurrency.md) |
| US 55 | [10](issues/10-proposal-concurrency.md) |
| US 56 | [03](issues/03-organized-reader-queries.md)、[11](issues/11-collections-snapshot.md)、[12](issues/12-ai-review-authority.md)、[15](issues/15-ordered-reading-list-writes.md)、[44](issues/44-http-organization-migration.md) |
| US 57 | [11](issues/11-collections-snapshot.md)、[12](issues/12-ai-review-authority.md) |
| US 58 | [11](issues/11-collections-snapshot.md)、[12](issues/12-ai-review-authority.md)、[15](issues/15-ordered-reading-list-writes.md)、[44](issues/44-http-organization-migration.md) |
| US 59 | [03](issues/03-organized-reader-queries.md)、[13](issues/13-franchise-durable-repair.md) |
| US 60 | [13](issues/13-franchise-durable-repair.md) |
| US 61 | [13](issues/13-franchise-durable-repair.md) |
| US 62 | [13](issues/13-franchise-durable-repair.md) |
| US 63 | [14](issues/14-franchise-cascade-facts.md) |
| US 64 | [14](issues/14-franchise-cascade-facts.md) |
| US 65 | [13](issues/13-franchise-durable-repair.md) |
| US 66 | [13](issues/13-franchise-durable-repair.md) |
| US 67 | [13](issues/13-franchise-durable-repair.md)、[14](issues/14-franchise-cascade-facts.md) |
| US 68 | [13](issues/13-franchise-durable-repair.md)、[14](issues/14-franchise-cascade-facts.md) |
| US 69 | [13](issues/13-franchise-durable-repair.md) |
| US 70 | [13](issues/13-franchise-durable-repair.md) |
| US 71 | [13](issues/13-franchise-durable-repair.md) |
| US 72 | [24](issues/24-library-create-update.md)、[45](issues/45-http-library-runs-migration.md) |
| US 73 | [24](issues/24-library-create-update.md) |
| US 74 | [24](issues/24-library-create-update.md) |
| US 75 | [24](issues/24-library-create-update.md)、[25](issues/25-relocation-process-storage.md) |
| US 76 | [24](issues/24-library-create-update.md)、[25](issues/25-relocation-process-storage.md) |
| US 77 | [22](issues/22-library-delete-reservation.md) |
| US 78 | [22](issues/22-library-delete-reservation.md) |
| US 79 | [23](issues/23-library-delete-drain.md) |
| US 80 | [22](issues/22-library-delete-reservation.md)、[23](issues/23-library-delete-drain.md) |
| US 81 | [22](issues/22-library-delete-reservation.md)、[23](issues/23-library-delete-drain.md) |
| US 82 | [22](issues/22-library-delete-reservation.md)、[23](issues/23-library-delete-drain.md) |
| US 83 | [22](issues/22-library-delete-reservation.md)、[23](issues/23-library-delete-drain.md) |
| US 84 | [22](issues/22-library-delete-reservation.md)、[23](issues/23-library-delete-drain.md) |
| US 85 | [23](issues/23-library-delete-drain.md) |
| US 86 | [23](issues/23-library-delete-drain.md) |
| US 87 | [23](issues/23-library-delete-drain.md) |
| US 88 | [22](issues/22-library-delete-reservation.md)、[23](issues/23-library-delete-drain.md) |
| US 89 | [14](issues/14-franchise-cascade-facts.md)、[23](issues/23-library-delete-drain.md) |
| US 90 | [16](issues/16-book-content-pages.md)、[17](issues/17-book-download-handles.md)、[45](issues/45-http-library-runs-migration.md) |
| US 91 | [16](issues/16-book-content-pages.md) |
| US 92 | [16](issues/16-book-content-pages.md) |
| US 93 | [17](issues/17-book-download-handles.md) |
| US 94 | [16](issues/16-book-content-pages.md) |
| US 95 | [16](issues/16-book-content-pages.md) |
| US 96 | [16](issues/16-book-content-pages.md) |
| US 97 | [16](issues/16-book-content-pages.md) |
| US 98 | [18](issues/18-comicinfo-publication.md) |
| US 99 | [18](issues/18-comicinfo-publication.md) |
| US 100 | [18](issues/18-comicinfo-publication.md) |
| US 101 | [18](issues/18-comicinfo-publication.md) |
| US 102 | [16](issues/16-book-content-pages.md)、[17](issues/17-book-download-handles.md)、[18](issues/18-comicinfo-publication.md) |
| US 103 | [18](issues/18-comicinfo-publication.md) |
| US 104 | [18](issues/18-comicinfo-publication.md) |
| US 105 | [18](issues/18-comicinfo-publication.md) |
| US 106 | [19](issues/19-external-root-boundary.md) |
| US 107 | [19](issues/19-external-root-boundary.md) |
| US 108 | [19](issues/19-external-root-boundary.md)、[20](issues/20-transfer-cancellation.md) |
| US 109 | [19](issues/19-external-root-boundary.md)、[20](issues/20-transfer-cancellation.md)、[45](issues/45-http-library-runs-migration.md) |
| US 110 | [20](issues/20-transfer-cancellation.md) |
| US 111 | [20](issues/20-transfer-cancellation.md) |
| US 112 | [20](issues/20-transfer-cancellation.md) |
| US 113 | [20](issues/20-transfer-cancellation.md) |
| US 114 | [21](issues/21-transfer-pause-resume.md) |
| US 115 | [21](issues/21-transfer-pause-resume.md) |
| US 116 | [21](issues/21-transfer-pause-resume.md) |
| US 117 | [21](issues/21-transfer-pause-resume.md) |
| US 118 | [20](issues/20-transfer-cancellation.md)、[21](issues/21-transfer-pause-resume.md) |
| US 119 | [20](issues/20-transfer-cancellation.md) |
| US 120 | [20](issues/20-transfer-cancellation.md) |
| US 121 | [20](issues/20-transfer-cancellation.md)、[21](issues/21-transfer-pause-resume.md) |
| US 122 | [21](issues/21-transfer-pause-resume.md) |
| US 123 | [27](issues/27-scan-ingestion.md)、[45](issues/45-http-library-runs-migration.md) |
| US 124 | [27](issues/27-scan-ingestion.md) |
| US 125 | [28](issues/28-watch-ingestion-coordination.md)、[45](issues/45-http-library-runs-migration.md) |
| US 126 | [27](issues/27-scan-ingestion.md)、[28](issues/28-watch-ingestion-coordination.md) |
| US 127 | [28](issues/28-watch-ingestion-coordination.md) |
| US 128 | [28](issues/28-watch-ingestion-coordination.md) |
| US 129 | [27](issues/27-scan-ingestion.md)、[28](issues/28-watch-ingestion-coordination.md) |
| US 130 | [26](issues/26-book-identity-matching.md) |
| US 131 | [26](issues/26-book-identity-matching.md) |
| US 132 | [26](issues/26-book-identity-matching.md) |
| US 133 | [04](issues/04-protocol-reader-queries.md)、[26](issues/26-book-identity-matching.md) |
| US 134 | [26](issues/26-book-identity-matching.md)、[27](issues/27-scan-ingestion.md) |
| US 135 | [27](issues/27-scan-ingestion.md)、[28](issues/28-watch-ingestion-coordination.md) |
| US 136 | [25](issues/25-relocation-process-storage.md) |
| US 137 | [30](issues/30-configuration-effects.md)、[46](issues/46-http-config-accounts-migration.md) |
| US 138 | [30](issues/30-configuration-effects.md)、[32](issues/32-settings-edit-session.md) |
| US 139 | [30](issues/30-configuration-effects.md) |
| US 140 | [31](issues/31-runtime-resource-ownership.md) |
| US 141 | [31](issues/31-runtime-resource-ownership.md) |
| US 142 | [25](issues/25-relocation-process-storage.md)、[30](issues/30-configuration-effects.md) |
| US 143 | [30](issues/30-configuration-effects.md)、[32](issues/32-settings-edit-session.md) |
| US 144 | [31](issues/31-runtime-resource-ownership.md) |
| US 145 | [31](issues/31-runtime-resource-ownership.md) |
| US 146 | [29](issues/29-metadata-consumer-ports.md)、[46](issues/46-http-config-accounts-migration.md) |
| US 147 | [29](issues/29-metadata-consumer-ports.md) |
| US 148 | [29](issues/29-metadata-consumer-ports.md) |
| US 149 | [29](issues/29-metadata-consumer-ports.md) |
| US 150 | [29](issues/29-metadata-consumer-ports.md) |
| US 151 | [42](issues/42-bootstrap-expand.md)、[47](issues/47-bootstrap-contract.md) |
| US 152 | [42](issues/42-bootstrap-expand.md)、[43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[46](issues/46-http-config-accounts-migration.md)、[47](issues/47-bootstrap-contract.md) |
| US 153 | [31](issues/31-runtime-resource-ownership.md)、[42](issues/42-bootstrap-expand.md) |
| US 154 | [31](issues/31-runtime-resource-ownership.md)、[42](issues/42-bootstrap-expand.md) |
| US 155 | [36](issues/36-task-observation-contracts.md) |
| US 156 | [37](issues/37-run-declarations.md) |
| US 157 | [37](issues/37-run-declarations.md) |
| US 158 | [37](issues/37-run-declarations.md) |
| US 159 | [37](issues/37-run-declarations.md) |
| US 160 | [37](issues/37-run-declarations.md) |
| US 161 | [37](issues/37-run-declarations.md)、[45](issues/45-http-library-runs-migration.md) |
| US 162 | [36](issues/36-task-observation-contracts.md) |
| US 163 | [36](issues/36-task-observation-contracts.md)、[43](issues/43-http-reader-query-migration.md)、[44](issues/44-http-organization-migration.md)、[45](issues/45-http-library-runs-migration.md)、[47](issues/47-bootstrap-contract.md) |
| US 164 | [37](issues/37-run-declarations.md) |
| US 165 | [33](issues/33-offline-feature.md) |
| US 166 | [33](issues/33-offline-feature.md) |
| US 167 | [33](issues/33-offline-feature.md) |
| US 168 | [33](issues/33-offline-feature.md) |
| US 169 | [32](issues/32-settings-edit-session.md) |
| US 170 | [32](issues/32-settings-edit-session.md) |
| US 171 | [32](issues/32-settings-edit-session.md) |
| US 172 | [32](issues/32-settings-edit-session.md) |
| US 173 | [08](issues/08-manual-metadata-usecase.md) |
| US 174 | [02](issues/02-catalog-reader-pagination.md)、[41](issues/41-composite-queries-cache.md) |
| US 175 | [12](issues/12-ai-review-authority.md)、[44](issues/44-http-organization-migration.md) |
| US 176 | [12](issues/12-ai-review-authority.md) |
| US 177 | [34](issues/34-catalog-query-action-effects.md) |
| US 178 | [08](issues/08-manual-metadata-usecase.md)、[09](issues/09-bulk-metadata-usecase.md)、[12](issues/12-ai-review-authority.md)、[34](issues/34-catalog-query-action-effects.md) |
| US 179 | [19](issues/19-external-root-boundary.md)、[36](issues/36-task-observation-contracts.md) |
| US 180 | [24](issues/24-library-create-update.md) |
| US 181 | [35](issues/35-query-failure-ui.md)、[41](issues/41-composite-queries-cache.md) |
| US 182 | [35](issues/35-query-failure-ui.md)、[41](issues/41-composite-queries-cache.md) |
| US 183 | [34](issues/34-catalog-query-action-effects.md)、[35](issues/35-query-failure-ui.md)、[41](issues/41-composite-queries-cache.md) |
| US 184 | [38](issues/38-account-initialization.md)、[39](issues/39-account-administration.md)、[40](issues/40-credentials-sessions.md)、[46](issues/46-http-config-accounts-migration.md) |
| US 185 | [38](issues/38-account-initialization.md) |
| US 186 | [39](issues/39-account-administration.md) |
| US 187 | [39](issues/39-account-administration.md) |
| US 188 | [38](issues/38-account-initialization.md)、[39](issues/39-account-administration.md)、[40](issues/40-credentials-sessions.md) |
| US 189 | [40](issues/40-credentials-sessions.md) |
| US 190 | [38](issues/38-account-initialization.md) |
| US 191 | [26](issues/26-book-identity-matching.md)、[38](issues/38-account-initialization.md)、[40](issues/40-credentials-sessions.md) |
| US 192 | [38](issues/38-account-initialization.md)、[40](issues/40-credentials-sessions.md)、[46](issues/46-http-config-accounts-migration.md) |
| US 193 | [38](issues/38-account-initialization.md)、[39](issues/39-account-administration.md)、[40](issues/40-credentials-sessions.md) |
| US 194 | [48](issues/48-integrated-acceptance.md) |
| US 195 | [48](issues/48-integrated-acceptance.md) |
| US 196 | [18](issues/18-comicinfo-publication.md)、[21](issues/21-transfer-pause-resume.md)、[48](issues/48-integrated-acceptance.md) |
| US 197 | [25](issues/25-relocation-process-storage.md)、[42](issues/42-bootstrap-expand.md)、[48](issues/48-integrated-acceptance.md) |
| US 198 | [48](issues/48-integrated-acceptance.md) |
| US 199 | [17](issues/17-book-download-handles.md)、[48](issues/48-integrated-acceptance.md) |
| US 200 | [48](issues/48-integrated-acceptance.md) |
| US 201 | [48](issues/48-integrated-acceptance.md) |
| US 202 | [07](issues/07-reader-model-contract.md)、[47](issues/47-bootstrap-contract.md)、[48](issues/48-integrated-acceptance.md) |
| US 203 | [48](issues/48-integrated-acceptance.md) |
| US 204 | [48](issues/48-integrated-acceptance.md) |

规格映射表示有承接工单，不表示故事已经实现或验证通过。最后验收还须逐条核对执行证据。
