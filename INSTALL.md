# Workloom 安装与项目接入

本文即安装器：可以直接按序自己执行，也可以整体发给编码 Agent
（Codex / Claude Code / OpenCode 等）。装的是 **workloom 二进制**（兼容别名 `devsys`，每
台机器一次），接的是**当前项目目录**（每个项目一次）——两步
作用域不同，不要混在一起。

版本号有意不写在本文里：涉及 release 的步骤一律以
<https://github.com/JAYY513/Workloom/releases/latest> 页面上的 tag 为准，
这样本文不需要随发版改动。

---

## §1 安装 workloom（每台机器一次）

完成标准：`workloom --version` 能输出版本号。

**选哪条**（按环境选，不用比较）：

- **Windows x64 + 已装 Node.js ≥ 18** → **1d（npm）**：`npm install -g @kaki317/workloom`，一条命令，升级就是重跑同一条。
- **其他平台（macOS / Linux / Windows ARM64）** → **1a（Release 脚本）**：npm 还没有这些平台的平台包，见 1d。
- 已装 Go、或不想下载脚本 / 要自己构建 → 1b / 1c。

### 1a. Release 脚本（macOS / Linux / Windows ARM64 的默认路径，约 10 秒）

1. 打开 <https://github.com/JAYY513/Workloom/releases/latest>；
2. 下载 `install.sh`（Windows PowerShell 用 `install.ps1`）；
3. 校验哈希（不匹配就停下，不要执行）：
   - Linux / macOS / Git Bash：`sha256sum install.sh`，对照同页 `checksums.txt`；
   - Windows PowerShell：`(Get-FileHash install.ps1 -Algorithm SHA256).Hash.ToLower()`；
4. 执行（`<tag>` 用第 1 步页面上的版本号）：
   - `bash install.sh --tag <tag>`
   - PowerShell：`powershell -NoProfile -ExecutionPolicy Bypass -File install.ps1 -Tag <tag> -AddToPath`
5. 自证：`workloom --version`（旧脚本用 `devsys --version` 同样有效）。

说明：本仓库为公开仓库（MIT）。已登录 `gh` 时脚本优先用它下载，
否则匿名拉取 Release 资产。下载或校验失败才回退
`git clone --branch <tag> + go build`（本机需 Go + Git）。
私有 fork 才需要先 `gh auth login`。
`install.ps1` 安装到 `%LOCALAPPDATA%\workloom\`（同时装入兼容别名 `devsys`），且只改写 User PATH。
Windows 上运行 `install.sh` 请用 Git Bash；若 `bash.exe` 解析到 WSL，
脚本会按 Linux 分支处理（WSL 内通常没有 Go/Git），不是脚本故障。

### 1b. 已装 Go：一行直装

公开模块，不要设 `GOPRIVATE`（那会跳过公共校验和数据库）：

```bash
go install github.com/JAYY513/Workloom/cmd/workloom@latest
```

（`@latest` 解析为最新的语义化 tag。只有私有 fork 才设
`GOPRIVATE=github.com/JAYY513/Workloom`。）

### 1c. 源码构建（兜底，仓库已提交 `vendor/`，可离线）

```bash
git clone https://github.com/JAYY513/Workloom.git
cd Workloom
# Linux / macOS
GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/workloom ./cmd/workloom
# Windows（产物带 .exe 扩展名，Git Bash / PowerShell / cmd 均可直接执行）
GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/workloom.exe ./cmd/workloom
```

### 1d. npm（Windows x64 推荐，一条命令）

```bash
npm install -g @kaki317/workloom   # 需要 Node.js >= 18
workloom --version
```

不替代 1a–1c。包名是 `@kaki317/workloom`（scoped），npm bin 只有 `workloom`；平台二进制随平台包一起装（`optionalDependencies`），安装时不下载 exe。

**当前只发布了 `win32-x64`**：macOS / Linux / Windows ARM64 上安装会缺少平台二进制，运行时被明确拒绝（不会去下载）——这些平台先用 1a–1c，等对应平台包发布后同一条命令即可用。其余平台包的 tgz 一直随 Release 附发（`dist/npm/*.tgz`），发布步骤见 [docs/发布流程.md](docs/发布流程.md) §5b。

`npx` 不作为主安装路径（冷启动要联网、版本随 npx 缓存漂移）；MCP 片段里的 npx 行只是可选补充。

## §2 接入项目（每个项目一次）

**接入目录：目标项目目录。** 若该目录已是 git 仓库，建议在仓库根接入；
非 Git 原型可直接 init。不要在已经含 `.devsys/` 的目录里面再套一层。

一条命令（幂等；已有文件一律不动）：

```bash
workloom setup                                  # 一条命令完成整段接入
```

等价的分步命令（`setup` 内部按序执行的就是这些；不含 MCP 客户端注册）：

```bash
workloom init                                   # 创建 .devsys/ + 项目级 .gitattributes（幂等）
workloom workflow init --template quick-fix     # 起步工作流；完整模板列表见该命令的用法输出
workloom wire                                   # AGENTS.md 纪律块 + .agents 技能文件（默认含 skill）
workloom wire --check                           # 接入检查，预期全绿
workloom prime                                  # 读取项目状态：事实 + 在飞工作 + 推荐下一步
```

`setup` 不写 MCP 客户端配置。注册是另一步，默认只预览：

```bash
workloom mcp install                             # 预览，不写。注册名保持 devsys
workloom mcp install --apply                     # 写入检测到的客户端（Codex / Claude Code / OpenCode）
workloom mcp install --apply --force             # 只替换已有 devsys 条目
```

无法安全合并（JSONC，或 Codex 行内 `mcp_servers` 表）时拒绝，即使用 `--force`。
改用 `workloom wire --print-mcp <codex|claude|opencode>` 手贴。

`--apply` 写完会**现场起一次** `mcp serve` 做启动检查（完成 MCP 握手并列工具面），报告形如
`probe: ok (19 tools in 320ms)`；起不来时报 `probe: failed: …` 并**以退出码 3 结束**——配置已写入，
但注册的服务器起不来，等于没接上。环境不允许起子进程时用 `DEVSYS_MCP_PROBE=0` 显式跳过
（报告写 `skipped`，不会被当成通过）。

之后检查项目蓝图：已配置则继续；**未配置则询问项目目标，不要假设**——
蓝图写入命令是 `workloom project update --blueprint-artifact <artifact-id>`。

## 给 Agent 的提示词

README「方式 A」与此处同款的稳定指针（不随版本变化）：

```text
帮我在当前项目接入 Workloom（https://github.com/JAYY513/Workloom）：

1. 若 workloom 未安装（workloom --version 无输出）：按该仓库 INSTALL.md 的
   §1 安装（releases/latest 取脚本，校验 checksums.txt 后执行），装完自证版本。
2. 在目标项目目录执行 `workloom setup`（该目录已是 git 仓则建议在仓库根；
   非 Git 原型也可直接接入；一步完成接入并自检，失败即停）。
任一步失败就停下报告，不要跳过哈希校验，不要直接修改 .devsys/ 内的受管文件
（所有写入走 workloom CLI 或 MCP）。
```
