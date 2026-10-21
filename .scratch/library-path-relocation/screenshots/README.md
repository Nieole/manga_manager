# 界面证据

`preview.png` 是真实服务中检查预览的实拍，对应 `7c827db`，覆盖资料库入口、目标位置、检查结果与分页明细布局。

`preview-policy.png` 对应 `1785575`，使用独立演示数据库和临时漫画目录。只读检查完成后，策略页展示自定义策略的四项并发值与三个开关的前后值；不包含用户实际资料库或凭据。

`commit-confirm.png`、`commit-completed.png` 和 `commit-reader.png` 对应 `4e49752` 源码的真实 HTTP 后端与 Vite 前端联调，分别展示正常计划确认、持久完成结果和沿原书 ID 在新路径阅读。演示库使用临时配置、假账户及生成的 CBZ；新嵌入式演示程序被智能应用控制阻止，没有作为成功启动的证据。

`repair-source-required.png`、`repair-confirm.png`、`repair-completed.png` 和 `repair-reader.png` 使用 `5222f3d` 的 Vite 前端与 `719b609` 编译的 HTTP 后端联调；这两个提交之间的 Go 源码相同。临时演示库的当前根已是 `repair-target`，系列与书仍在 `target`：界面要求明确选择原根，然后展示三根预览、完成结果及沿原书 ID 打开的实际页图。资料库、系列、书的 ID 均保持为 1，阅读页图加载尺寸为 400 × 600；截图使用假账户和生成的 CBZ，不涉及用户资料库。

`anomaly-details.png` 与 `anomaly-protected.png` 对应 `cfc49a7` 的真实 HTTP 后端及 Vite 前端联调。独立假资料库 ID 2 有原系列 ID 2 和两本书，目标缺少第二卷；前一张展示异常明细与尚未勾选的明确接受项，未接受时提交按钮禁用。明确接受后完成迁移并建立保护，后一张展示保留的原扫描设置 interval / 60 / cbz；实际自动模式变为 none，原记录均保留，源文件未被迁移操作移动。截图中的通用检查提示已由后续提交改为与异常可接受的行为一致；后续不可变操作 ID 的并发修复也不属于该实拍版本。截图只使用临时目录、假账户与生成的 CBZ。

`policy-confirm.png` 和 `policy-completed.png` 对应 `0962f35` 源码的真实 HTTP 后端与 Vite 前端联调。临时演示资料库 3、系列 3 使用假账户和生成的 CBZ，从 `policy-source` 映射至 `policy-target`；预览展示一条外接机械盘显式策略的全部 IO 值，提交结果为完成。集成负责人另核对临时配置文件中的策略路径已为 `policy-target`。截图不涉及用户实际资料库；最终零策略磁盘事实修复的回归证据归 `validation-08.md`，这两张截图不作为该修复的运行证据。

`restoration-rechecked.png`、`restoration-risk.png` 与 `restoration-completed.png` 使用 `ebd3f38` 的 Vite 前端和 `a1abc58` 编译的真实 HTTP 后端联调；这两个提交之间的 Go 源码相同。独立假资料库 2 的目标缺少第二卷，原自动设置为 interval / 60 / cbz。重查得到一个异常、零阻止项且保护继续保留；管理员选择 none / 60 / cbz，分别确认剩余异常可能造成后续记录和关联数据删除的风险以及主动解除保护，再得到持久完成结果。界面明确说明恢复不会立即扫描或清理。截图使用临时配置、假账户和生成的 CBZ，不涉及用户资料库；后续任务准入冲突与故障信息隐私修复的验证归 `validation-07.md`，不以这些截图证明其运行结果。
