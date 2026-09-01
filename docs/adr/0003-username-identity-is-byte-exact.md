# 账户身份按字节精确，不折叠大小写

站点账户的用户名是**字节精确**的：`Alice` 与 `alice` 是两个独立账户。唯一性判据只有一条——
`users.username` 上的 UNIQUE 约束（SQLite 默认 BINARY 排序规则）。查询、建号判重、
登录失败计数的分桶键都必须用这同一把尺子；任何一处折叠大小写，两个独立账户就会在那一处被算作一个。

## 为什么不往「大小写不敏感」统一

不敏感看起来更友好，也更像本来的意图，但它要求 `username` 上有一条 NOCASE 唯一索引，
而这条索引在存量库上建不上去：

- 已经存在 `Alice` 与 `alice` 的库，`CREATE UNIQUE INDEX ... COLLATE NOCASE` 直接报
  `UNIQUE constraint failed`。本仓库的迁移是硬失败：`Migrate` 返回错误，`cmd/server` 随即
  `os.Exit(1)`，既没有跳过某一步的机制，也没有降级路径。升级的表现就是服务再也起不来，
  而自托管用户手上没有修它的手段（只能拿 sqlite3 手改库）。
- 退一步「只把查询改成 NOCASE、约束建不上就算了」更糟：约束没建成时
  `WHERE username = ? COLLATE NOCASE` 会同时命中那两行，登录落到哪个账户上取决于行序——
  收藏、阅读进度、离线授权全部按 `user_id` 分家，这是串号，比原来的缺口严重。
- 「不敏感」在这两层里也不是同一条规则：SQLite 的 NOCASE 只折叠 ASCII A–Z，Go 的
  `strings.ToLower` 按 Unicode 折叠（`İ` → `i`）。就算两边都改成不敏感，口径依然是两套。
  同一个坑在 `database.gramMatchExpr` 上已经踩过一次。

代价是明确的：管理员可以建出 `Admin` 与 `admin` 这种易混账户，本决策不拦。真要拦，
该在建号入口做**混淆名**校验（一个独立问题），而不是把身份口径改成不敏感。

## 为什么记下来

「用户名当然应该大小写不敏感，这更安全」是一个表面正确、每隔一段时间就会被重新提出的改动，
而拦住它的两个事实都不在 `users.go` 里：一个在迁移的失败处理方式上，一个在 SQLite 与 Go
对「不敏感」的定义分歧上。没有这条记录，下一次评审会再提一次，并且很可能只改查询不改约束。

`database.TestUsernameIdentityIsByteExact` 是这条决策的落锁点：改成 NOCASE 会让它失败。
