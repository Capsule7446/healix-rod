# AGENTS.md

使用简体中文，先给结论，再给必要依据。改动不得提交或推送，除非用户明确授权。

## 项目定位

本仓库是 `github.com/Capsule7446/healix-core` 的官方 go-rod 基础设施适配器，不拥有领域规则。

## 架构规则

```text
Healix composition root -> healix-rod -> healix-core domain ports
                                |
                                +-> go-rod / Chromium / CDP
```

- 根包实现 Core `node.Driver` 等端口；`sampler` 实现 Sampling 技术适配。
- 禁止 Core 反向依赖本模块。
- 禁止引入 Healix internal、Wails、SQLite、桌面路径或 View DTO。
- `RawPage`、`Expose` 等技术扩展只允许 Infrastructure 消费，不得进入 Core/Application 契约。
- selector 全部明确 NotFound 后才返回 `node.ErrElementNotFound`；不得把 transport/context 错误翻译成缺失。

## 验证

```bash
gofmt -l .
go test -short ./...
go test ./... -count=1
go test -race ./...
go vet ./...
go build ./...
```

全量测试会启动真实 Chromium，首次运行可能下载浏览器。
