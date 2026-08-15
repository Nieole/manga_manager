// Package taskcontrol 提供任务的暂停闸门：任务体在可中断点调用 Wait 问一句「现在能继续吗」，
// 于是暂停停在下一个可中断点，而不丢弃已完成的工作。闸门经 context 传递，ctx 里没有闸门时
// Wait 不阻塞；本包没有取消入口，取消由 context 传播，暂停中的 Wait 也监听 ctx.Done()。
//
// 边界：本包不认识任务键、作用域、阶段与终态，任务的准入、落盘与推送属于 internal/api 的
// taskEngine——它创建闸门并决定何时 Pause/Resume；动磁盘前的存储令牌属于 internal/storageio
// 的 Scheduler.Acquire。本包只依赖标准库。
package taskcontrol
