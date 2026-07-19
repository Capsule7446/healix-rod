# healix-rod

`healix-rod` 是 HealiX Core 的 go-rod 浏览器适配器。它将 Chromium、CDP 和页面交互能力封装为 Core 定义的浏览器端口，并提供交互式采样子包。

## 包结构

### 根包

模块路径：`github.com/Capsule7446/healix-rod`

根包提供以下能力：

- `Driver`：启动和关闭 Chromium，打开页面，导航，执行脚本，等待网络空闲，以及截取视口图像。
- `Locate`：按 selector 优先级定位 CSS、XPath、文本、`data-testid` 和 ARIA role 元素。
- `Element`：读取元素状态，执行点击、输入、选择、悬停和稳定性等待。
- `Snapshot`：收集 DOM 候选节点，供 Core 的自愈逻辑评分。

### sampler 子包

模块路径：`github.com/Capsule7446/healix-rod/sampler`

`sampler` 提供受控浏览器和页面采样能力：

- 打开页面并保持采样脚本处于暂停状态。
- 开始或暂停普通交互采样。
- 捕获一次验证操作。
- 将页面协议数据校验并转换为 Core 的 `sampling.Capture`。
- 在关闭时移除页面脚本、绑定和浏览器资源。

## 安装

```bash
go get github.com/Capsule7446/healix-rod
```

项目依赖 `github.com/Capsule7446/healix-core`、`github.com/go-rod/rod` 和 `github.com/ysmood/gson`。本地开发 Core 时，可以在 `go.mod` 中使用：

```go
replace github.com/Capsule7446/healix-core => ../healix-core
```

## 使用 Driver

```go
package main

import (
	"context"
	"log"

	rodadapter "github.com/Capsule7446/healix-rod"
)

func main() {
	ctx := context.Background()
	driver, err := rodadapter.New(rodadapter.Options{Headless: true})
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := driver.Close(); err != nil {
			log.Print(err)
		}
	}()

	if err := driver.Navigate(ctx, "https://example.com"); err != nil {
		log.Fatal(err)
	}
}
```

`Options.BrowserPath` 可以指定已安装的 Chrome 或 Chromium。`Options.LocateTimeout` 控制每个 selector 的单次定位时间；未设置时使用默认值。

## 使用 sampler

```go
package main

import (
	"context"
	"log"

	"github.com/Capsule7446/healix-core/domain/sampling"
	"github.com/Capsule7446/healix-rod/sampler"
)

func captureHandler(capture sampling.Capture) (sampling.CaptureResult, error) {
	// 在应用层持久化或处理 capture。
	return sampling.CaptureResult{}, nil
}

func main() {
	ctx := context.Background()
	browser, err := sampler.NewControlled(sampler.Options{Headless: true})
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := browser.Close(); err != nil {
			log.Print(err)
		}
	}()

	if err := browser.Open(ctx, "https://example.com"); err != nil {
		log.Fatal(err)
	}
	if err := browser.BeginCapture(ctx, captureHandler); err != nil {
		log.Fatal(err)
	}
}
```

`BeginCapture` 需要非空的 `sampling.CaptureHandler`。`PauseCapture` 会停止页面事件转发，但不会关闭浏览器或页面。`ArmValidationCapture` 需要采样正在运行，并只捕获下一次验证操作。

## 错误和边界

- 只有所有 selector 都明确找不到元素时，`Driver.Locate` 才返回 `node.ErrElementNotFound`。
- 调用方取消、浏览器连接错误、无效 selector 和脚本异常会保留为独立错误，不会被转换为元素缺失。
- `RawPage`、`Expose`、`EvalOnNewDocument` 和 `EvalScript` 是基础设施扩展，只应由浏览器适配层或基础设施协作者使用。
- `sampler` 会校验协议版本、字段格式和节点数据，并拒绝未知 JSON 字段。
- Core 领域类型和应用服务不应依赖 go-rod、CDP 或页面 DTO。

## Go 质量检查

仓库提供 [Makefile](Makefile) 统一本地命令：

```bash
make fmt-check
make test-short
make vet
make build
make race
make coverage
make vulncheck
make gosec
make check
```

`make race` 需要 `CGO_ENABLED=1` 和可用的 C 编译器。完整浏览器测试可以通过 `BrowserPath` 指向 `.devtools/chromium/Application/chrome.exe`；`.devtools/chromium/`、`.devtools/bin/`、覆盖率和安全报告均已加入 `.gitignore`。

## 许可证

许可证信息以仓库根目录的许可证文件为准。
