# Workloom 安装与项目接入

本文即安装器：可以直接按序自己执行，也可以整体发给编码 Agent
（Codex / Claude Code / OpenCode 等）。装的是 **devsys 二进制**（每
台机器一次），接的是**当前 git 仓库项目**（每个项目一次）——两步
作用域不同，不要混在一起。

版本号有意不写在本文里：涉及 release 的步骤一律以
<https://github.com/JAYY513/Workloom/releases/latest> 页面上的 tag 为准，
这样本文不需要随发版改动。

---

## §1 安装 devsys（每台机器一次）

完成标准：`devsys --version` 能输出版本号。

### 1a. Release 脚本（推荐，约 10 秒）

1. 打开 <https://github.com/JAYY513/Workloom/releases/latest>；
2. 下载 `install.sh`（Windows PowerShell 用 `install.ps1`）；
3. 校验哈希（不匹配就停下，不要执行）：
   - Linux / macOS / Git Bash：`sha256sum install.sh`，对照同页 `checksums.txt`；
   - Windows PowerShell：`(Get-FileHash install.ps1 -Algorithm SHA256).Hash.ToLower()`；
4. 执行（`<tag>` 用第 1 步页面上的版本号）：
   - `bash install.sh --tag <tag>`
   - PowerShell：`powershell -NoProfile -ExecutionPolicy Bypass -File install.ps1 -Tag <tag> -AddToPath`
5. 自证：`devsys --version`。

说明：本仓库为私有仓库，脚本优先经 `gh` 鉴权下载（先 `gh auth login`）；
未登录时自动回退 `git clone --branch <tag> + go build`（本机需 Go + Git）。
`install.ps1` 安装到 `%LOCALAPPDATA%\devsys\`，且只改写 User PATH。
Windows 上运行 `install.sh` 请用 Git Bash；若 `bash.exe` 解析到 WSL，
脚本会按 Linux 分支处理（WSL 内通常没有 Go/Git），不是脚本故障。

### 1b. 已装 Go：一行直装

```bash
go env -w GOPRIVATE=github.com/JAYY513/Workloom
go install github.com/JAYY513/Workloom/cmd/devsys@latest
```

（`@latest` 解析为最新的语义化 tag；私有仓库靠 GOPRIVATE 直连，
跳过公共校验和数据库。）

### 1c. 源码构建（兜底，仓库已提交 `vendor/`，可离线）

```bash
git clone https://github.com/JAYY513/Workloom.git
cd Workloom
# Linux / macOS
GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/devsys ./cmd/devsys
# Windows（产物带 .exe 扩展名，Git Bash / PowerShell / cmd 均可直接执行）
GOPROXY=off GOFLAGS=-mod=vendor go build -o bin/devsys.exe ./cmd/devsys
```

## §2 接入项目（每个项目一次）

**前置门禁：当前目录必须就是目标 git 仓库的根目录**（`devsys init`
要求 git 仓库根）。不满足就停下来确认，不要在上级目录或子目录里初始化。

```bash
devsys init                                   # 创建 .devsys/ + 项目级 .gitattributes（幂等）
devsys workflow init --template quick-fix     # 起步工作流；另有 feature-development /
                                              # architecture-change / reference-template
devsys wire                                   # AGENTS.md 纪律块 + .agents 技能文件（默认含 skill）
devsys wire --check                           # 接入检查，预期全绿
devsys prime                                  # 读取项目状态：事实 + 在飞工作 + 推荐下一步
```

之后检查项目蓝图：已配置则继续；**未配置则询问项目目标，不要假设**——
蓝图写入命令是 `devsys project update --blueprint-artifact <artifact-id>`。

## 给 Agent 的提示词

README「方式 A」与此处同款的稳定指针（不随版本变化）：

```text
帮我在当前项目接入 Workloom（https://github.com/JAYY513/Workloom）：

1. 若 devsys 未安装（devsys --version 无输出）：按该仓库 INSTALL.md 的
   §1 安装（releases/latest 取脚本，校验 checksums.txt 后执行），装完自证版本。
2. 确认当前目录就是目标 git 仓库的根目录；不是就先停下问我。
   然后按 INSTALL.md 的 §2 完成项目接入。
任一步失败就停下报告，不要跳过哈希校验，不要直接修改 .devsys/ 内的受管文件
（所有写入走 devsys CLI 或 MCP）。
```
