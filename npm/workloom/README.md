# workloom (npm distribution)

Installs the **workloom** CLI — the Go binary from
[github.com/JAYY513/Workloom](https://github.com/JAYY513/Workloom). This package
is a distribution wrapper: it execs the platform binary installed with it as an
optional dependency. It never downloads a binary at install time and has no
postinstall script.

```bash
npm install -g @kaki317/workloom
workloom --version
workloom setup          # inside your project directory: init → workflow → wire → checks
```

**Platforms: `win32-x64` today.** macOS, Linux and Windows ARM64 have no
platform package yet — `npm install` succeeds there, but running `workloom`
refuses with a message instead of downloading anything. On those platforms use
the release script or `go install`
(see [INSTALL.md](https://github.com/JAYY513/Workloom/blob/master/INSTALL.md) §1).

`npx --yes @kaki317/workloom …` works too, but it is not the recommended path:
cold starts need the network and the version follows the npx cache. For MCP
clients, register the installed binary (`workloom mcp install`), not npx.

---

中文：本包是 `workloom` CLI 的 **npm 分发层**——按平台调用已构建的 Go 二进制，
安装时不下载、无 postinstall 脚本。目前只发布 Windows x64 平台包，其他平台请按
[INSTALL.md](https://github.com/JAYY513/Workloom/blob/master/INSTALL.md) §1 用 Release
脚本或 `go install`。装完在项目目录里跑 `workloom setup` 完成接入（init → 起手工作流 →
wire → 各项检查），MCP 注册是显式的下一步 `workloom mcp install`。

MIT © JAYY513
