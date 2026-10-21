# 界面证据

`preview.png` 是真实服务中检查预览的实拍，对应 `7c827db`，覆盖资料库入口、目标位置、检查结果与分页明细布局。

`preview-policy.png` 对应 `1785575`，使用独立演示数据库和临时漫画目录。只读检查完成后，策略页展示自定义策略的四项并发值与三个开关的前后值；不包含用户实际资料库或凭据。

`commit-confirm.png`、`commit-completed.png` 和 `commit-reader.png` 对应 `4e49752` 源码的真实 HTTP 后端与 Vite 前端联调，分别展示正常计划确认、持久完成结果和沿原书 ID 在新路径阅读。演示库使用临时配置、假账户及生成的 CBZ；新嵌入式演示程序被智能应用控制阻止，没有作为成功启动的证据。

`repair-source-required.png`、`repair-confirm.png`、`repair-completed.png` 和 `repair-reader.png` 使用 `5222f3d` 的 Vite 前端与 `719b609` 编译的 HTTP 后端联调；这两个提交之间的 Go 源码相同。临时演示库的当前根已是 `repair-target`，系列与书仍在 `target`：界面要求明确选择原根，然后展示三根预览、完成结果及沿原书 ID 打开的实际页图。资料库、系列、书的 ID 均保持为 1，阅读页图加载尺寸为 400 × 600；截图使用假账户和生成的 CBZ，不涉及用户资料库。

`policy-confirm.png` 和 `policy-completed.png` 对应 `0962f35` 源码的真实 HTTP 后端与 Vite 前端联调。临时演示资料库 3、系列 3 使用假账户和生成的 CBZ，从 `policy-source` 映射至 `policy-target`；预览展示一条外接机械盘显式策略的全部 IO 值，提交结果为完成。集成负责人另核对临时配置文件中的策略路径已为 `policy-target`。截图不涉及用户实际资料库；最终零策略磁盘事实修复的回归证据归 `validation-08.md`，这两张截图不作为该修复的运行证据。
