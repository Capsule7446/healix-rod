# 公开契约

## 所有权

- `healix-core` 拥有 Driver/Element/Snapshot/Recorder 端口、领域错误与执行语义。
- `healix-rod` 拥有 go-rod、Chromium、CDP、selector 解析、等待策略与采样 wire ACL。
- Healix 宿主拥有桌面生命周期、设置、存储、网络证据持久化与 View 契约。

## 兼容性

- `Options`、`Driver`、`New` 和 `sampler` 导出 API 是本模块的 v0 公共表面。
- Core 端口的错误语义优先；底层 Rod 错误不得泄漏为领域状态。
- `RawPage` 与脚本绑定属于 Infrastructure 扩展，不承诺成为 Core/Application 契约。

## 非目标

- 不在本模块实现领域自愈算法、Run 状态机、Workspace 事务或桌面 UI。
- 不要求 Core 或第三方 Driver 依赖 go-rod 类型。
