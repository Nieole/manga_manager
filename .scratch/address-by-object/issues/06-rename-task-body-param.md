# 06 — 任务体形参改名

**What to build:** 同一个东西在两个包里叫两个名字：**运行句柄**在领域侧的形参已经叫 `handle`，
而 `internal/api` 的 17 个启动点仍叫 `tp`——旧类型名的缩写。新来的人要认两次。

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] `internal/api` 里任务体的**运行句柄**形参由 `tp` 改为 `handle`（约 74 处）
- [ ] 纯局部改名，不动任何签名的类型、不动任何行为
- [ ] 既有用例全绿即是验收，不新增用例
- [ ] 不重生 `generated.ts`（形参名不进契约）
- [ ] 后端门禁全绿
