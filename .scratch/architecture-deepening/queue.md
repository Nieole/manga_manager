# 全项目架构深化：工单索引

本页只提供索引，各票的交付结果和验收条件保存在独立文件中。实施依据为 [父规格](spec.md)，其状态保持 ready-for-agent；实施与门禁结果见各票及[集成记录](implementation.md)。

执行各票时以其状态和 Blocked by 为准。无前置阻塞的入口为 01、09、14、17，建议优先推进阅读方向；共有目录或相近编号不额外构成阻塞。

| 编号 | 工单 | 前置阻塞 |
| --- | --- | --- |
| 01 | [在线阅读位置的完整用例](issues/01-online-reading-position.md) | 无 |
| 02 | [统一离线批量与逐本补传](issues/02-offline-position-sync.md) | 01 |
| 03 | [批量读态与持久化未读保护](issues/03-bulk-read-state-reset-protection.md) | 01 |
| 04 | [批量已读活动与未知页数统计](issues/04-read-mark-activity-unknown-pages.md) | 03 |
| 05 | [KOReader 新报告与旧记录重放](issues/05-koreader-position-replay.md) | 01、03 |
| 06 | [共享阅读状态查询与继续阅读](issues/06-reading-projections-continue.md) | 01、03、04 |
| 07 | [阅读时长的完整用例](issues/07-reading-time-usecase.md) | 01 |
| 08 | [书签的完整用例](issues/08-reading-bookmarks-usecase.md) | 01 |
| 09 | [人工合集的完整用例](issues/09-manual-collections-usecase.md) | 无 |
| 10 | [智能书架与快照落地](issues/10-smart-collections-snapshot.md) | 09 |
| 11 | [系列关系与作品群重建](issues/11-relations-franchise-rebuild.md) | 09 |
| 12 | [AI 分组裁决与合集创建](issues/12-ai-grouping-adjudication.md) | 09 |
| 13 | [有序阅读清单与进度回填](issues/13-ordered-reading-lists.md) | 06 |
| 14 | [应用会话连接与普通刷新](issues/14-application-session-refresh.md) | 无 |
| 15 | [任务实况、历史与气泡投影](issues/15-task-observation-projections.md) | 14 |
| 16 | [藏书刷新与外部传输投影](issues/16-catalog-transfer-consistency.md) | 14 |
| 17 | [应用运行入口与历史保留清理](issues/17-application-run-entry-retention.md) | 无 |
| 18 | [手动、定时与监听扫描清理](issues/18-scan-watch-cleanup-orchestration.md) | 17 |
| 19 | [扫描封面批与封面运行](issues/19-cover-batch-run-handoff.md) | 17 |
| 20 | [资料库迁移与保护恢复编排](issues/20-relocation-restoration-orchestration.md) | 18 |
| 21 | [维护及 KOReader 校准运行发起](issues/21-maintenance-koreader-task-launch.md) | 05、18、19 |
| 22 | [外部库与 ComicInfo 磁盘用例发起](issues/22-external-comicinfo-task-launch.md) | 17 |
| 23 | [刮削与 AI 分组后台发起](issues/23-scrape-grouping-task-launch.md) | 17 |
| 24 | [进程装配与启动停止收口](issues/24-runtime-assembly-lifecycle-contract.md) | 18、19、20、21、22、23 |
| 25 | [四方向联合验收与归属核对](issues/25-integrated-acceptance-ownership.md) | 02、04、05、06、07、08、10、11、12、13、15、16、24 |

每票完成自身范围的规则迁移、旧副本清除和适用门禁。25 负责跨方向联合验收，不代替前序工单的独立完成条件。

各票仅在全部验收条件与适用门禁满足后关闭；集成代码、局部验证和完整验收分别记录。
