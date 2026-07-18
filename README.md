# healix-rod

`healix-rod` 是 HealiX Core 的官方 go-rod 浏览器适配器。它实现 `healix-core/domain/node` 定义的浏览器端口，并提供基于 Rod/CDP 的交互采样适配器。

## 边界

- 根包 `github.com/Capsule7446/healix-rod`：Driver、Element、Snapshot 与浏览器生命周期。
- 子包 `github.com/Capsule7446/healix-rod/sampler`：受控可见浏览器、页面脚本和采样 wire ACL。
- `healix-core` 不依赖本模块；应用通过 Core 端口使用 Driver。
- Wails、SQLite、桌面路径、录屏和 View Request/Response 不属于本模块。

## 使用

```go
driver, err := rodadapter.New(rodadapter.Options{Headless: true})
if err != nil {
	return err
}
defer driver.Close()

err = engine.RunProgram(ctx, program, engine.Config{
	Driver: driver,
})
```

迁移期通过同级目录联调：

```text
replace github.com/Capsule7446/healix-core => ../healix-core
replace github.com/Capsule7446/healix-rod => ../healix-rod
```

Core 发布正式版本后，应使用语义化版本并删除本地 `replace`。
