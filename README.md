# Workloom

独立于 Harness 的 Agent 开发基础设施。状态保存在项目仓库内（`.devsys/`），
通过 Git 在设备之间同步，不依赖跨项目共享数据库。

## 文档

| 文档 | 说明 |
|---|---|
| [方案](docs/原始文档/独立于Harness的Agent开发基础设施方案.md) | 规格与选型依据，冻结；只有故意偏离设计时才修改 |
| [实施计划](docs/原始文档/实施计划.md) | M0–M9 共 54 步，每步含产出、依赖与验收命令 |

## 当前进度

| 里程碑 | 状态 |
|---|---|
| M0.1 语言与工程骨架 | 已完成（Go 1.26，`devsys --version` 可跑） |
| M0.2 CLI 骨架与 `devsys init` | 已完成（2026-09-17；退出码 0/1/2/3、`--json`/`--quiet`、`init` 幂等、用户级注册表、非 Git 拒绝） |
| M0.3 文本存储基元 | 已完成（2026-09-17；原子写、JSONL 追加与断尾检测、稳定键序序列化、项目级写锁、乐观并发版本校验、可恢复跨文件事务；`gopkg.in/yaml.v3` 已 vendored） |
| M0.4 严格配置解析 | 已完成（2026-09-17；`project.yaml`/`config.yaml` 严格解析：未知键、类型错误、缺必填项逐条报 `文件:行号: 字段: 原因`；受管文件 `schema_version` 校验，未知版本拒绝写入、只读诊断可用；新增 `devsys config check` 与退出码 4） |
| M1.1 领域类型与序列化 | 已完成：八类模型、版本化 YAML/JSON 往返、稳定字节、config/init 统一 Project 类型 |
| M1.2 WorkItem 读写与 ID 分配 | 已完成：internal/workitem，`<前缀>-<序号>` CAS 分配、并发唯一、稳定排序、乐观更新 |
| M1.3 事件流（JSONL 按月分片） | 已完成：internal/events，`events/<YYYY-MM>.jsonl` 追加/过滤/评论线程，断尾检测阻断追加 |
| M1.4 Run 记录与上下文快照 | 已完成：internal/run，`run-<日期>-<序号>`，§11.3 ContextSnapshot 随 Run 往返 |
| M1.5 记录（Decision/Finding/Artifact） | 已完成：internal/record，一记录一文件；Artifact 版本链不可变追加 |
| M1.6 检索（文本扫描） | 已完成：internal/search + `devsys search`，无索引扫描，排除 `.cache/`、`local/` |
| M1.7 里程碑剧本与快照 | 已完成：PowerShell / Bash 剧本、真实领域层回读验收、13 文件示例快照 |

## 构建与验收

```sh
go build -o bin/devsys.exe ./cmd/devsys
bin/devsys.exe --version      # devsys 0.1.0-dev
bin/devsys.exe --help
bin/devsys.exe bogus; echo $? # 2（用法错误）

# 在 Git 仓库根目录初始化项目状态目录（幂等；重复执行不覆盖已有内容）
bin/devsys.exe init
bin/devsys.exe --json init    # 机器可读输出
bin/devsys.exe init; echo $?  # 非 Git 目录：3（前置条件错误）
```

`init` 创建 `.devsys/`（方案 §14.3，另含 `.devsys/.gitignore` 排除 `local/` 与 `.cache/`），
并在用户级注册表（`DEVSYS_CONFIG_DIR` 可覆盖）登记项目路径。

校验受管元数据文件（只读：不加锁、不写盘，未知 `schema_version` 时仍可运行，方案 §14.1）：

```sh
bin/devsys.exe config check
bin/devsys.exe --json config check    # 问题以结构化数组输出
bin/devsys.exe config check; echo $?  # 0 通过；4 解析/字段/版本问题；3 未初始化
```

有问题的文件会逐条给出 `文件:行号: 字段: 原因`（例如 `project.yaml:5: unknown_field: unknown key`）；
写命令（当前为 `init`）在任何改动前先校验既有受管文件，未知 `schema_version` 直接拒绝并提示迁移。

关键字检索（无数据库、无索引，直接扫描 `.devsys/` 文本，排除 `.cache/` 与 `local/`）：

```sh
bin/devsys.exe search <keyword>
bin/devsys.exe --json search <keyword>   # {ok,root,query,matches,total}
bin/devsys.exe search; echo $?           # 缺关键字：2（用法错误）
```

文本输出为 `<相对路径>:<行号>: <行内容>`；大小写不敏感子串匹配，单文件最多报 5 条命中行，
行内容截断 200 字节（UTF-8 安全）。实测 1000 文件（三分支目录 + 每 YAML 约 40B）并行扫描
约 0.6–0.7s（Windows/Defender 环境串行约 3.1s）；输出顺序与单线程遍历一致（路径+行号稳定排序）。

M1 完整剧本（Go 与 Git 必须在 PATH）：

```sh
# Windows PowerShell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m1.ps1
# Bash；Windows 请在 Git Bash 中执行（WSL 需自行安装 Go/Git）
bash scripts/smoke-m1.sh
```

脚本从仓库根构建 CLI 与 helper，在新临时 Git 项目中执行：初始化 → 创建 draft 任务 →
记录决策/发现、评论回复、Artifact 三版本与 Run 快照 → 乐观更新任务为 done → 检索及回读验证。
这里只验证 M1 原始持久化，不实现 M2 状态机或门禁。所有暂存状态文件必须为 UTF-8 且无 NUL。
默认清理临时目录；PowerShell `-Keep` / Bash `--keep` 保留项目并打印 `KEPT_PROJECT`。

`docs/examples/m1-devsys/` 是一次通过验收的 `.devsys/` 内容快照（13 个文本文件），不含本地锁、缓存和用户注册表。
可复制其内容到新 Git 仓库的 `.devsys/`，然后在该仓库运行 `devsys init` 补齐目录并登记注册表。
示例项目 ID 为 `demo-project`；用于真实项目时需一致替换各记录中的项目 ID 与示例内容。

测试与静态检查（`vendor/` 已提交，离线可跑）：

```sh
gofmt -l cmd internal   # 无输出
go vet ./...
go test ./...           # vendor 模式下不需要网络
```

带版本信息构建（发布用）：

```sh
go build -ldflags "-X workloom/internal/version.Version=0.1.0 -X workloom/internal/version.Commit=$(git rev-parse --short HEAD)" -o bin/devsys.exe ./cmd/devsys
```

## 目录结构

```
cmd/devsys/          命令入口（进程退出码）
internal/cli/        命令分发、全局开关、输出与退出码
internal/storage/    存储基元：原子写、JSONL、稳定序列化、项目锁、事务日志与恢复（方案 §15.2）
internal/config/     严格配置解析、错误定位与 schema_version 校验（方案 §14.1）
internal/domain/     Project/WorkItem/Run/Decision/Finding/Event/Artifact/Approval 与版本化序列化
internal/project/    init 与项目内布局（后续：领域 / 访问 / 执行）
internal/registry/   用户级项目路径注册表（方案 §14.4）
internal/version/    构建标识（可用 -ldflags 覆盖）
vendor/              依赖副本（gopkg.in/yaml.v3），保证干净机器离线构建
docs/原始文档/       方案与实施计划（源文档，不再拆分）
docs/开发记录.md     实施中的问题、偏差、决策与遗留事项（本地记录）
bin/                 构建产物（不提交）
```

后续面向使用者的手册与迁移说明放在 `docs/` 下（不进 `docs/原始文档/`）。

## 依赖策略

- 唯一外部依赖是 `gopkg.in/yaml.v3`，已 `go mod vendor` 并提交 `vendor/`；干净机器无需网络：
  `GOFLAGS=-mod=vendor GOPROXY=off go build ./...`。
- 新增或升级依赖：`GOPROXY=https://goproxy.cn go get <module>`（本机直连 `proxy.golang.org` 实测超时；
  也可 `HTTPS_PROXY=http://127.0.0.1:10808` 走本地代理），随后 `go mod tidy && go mod vendor` 一起提交。
- 锁与事务恢复材料只依赖标准库系统调用（Windows 动态调用 LockFileEx，POSIX 用 flock），不引入锁库。

## 约定

- 核心用 Go；M7 的工作区视图用 TypeScript/React 实现，只读渲染、不碰进程。
- 一切项目状态写成可 diff、可合并的文本（YAML / Markdown / JSONL）。
- 不使用数据库；任何缓存都是可删除、可重建的普通文件。
- 行尾统一 LF（见 `.gitattributes`），保证状态文件跨机器合并不产生幽灵 diff。
- 时间估算不进入状态输出（见方案 §7.4）。

### 领域文件契约（M1.1）

- 受管文档 `schema_version: 1`；领域 YAML/JSON 解码拒绝未知版本、未知字段和第二份文档。
- Project 包含方案 §5.1 全字段；`id`、`name`、`schema_version` 必填，旧最小元数据仍可读取。时间使用 RFC3339，写入者提供 UTC。
- `state/current.yaml` 严格接受 summary/risks/blockers/next_focus；`state/milestones.yaml` 接受 milestones（id/name/status 对象列表），不再放行未知键。
- `config.yaml` 仍只接受 schema_version。业务配置在相应里程碑扩展。
- `null` 与空集合保留差异；Run verification.advanced 的 null/false/true 区分未验证、失败与成功。
- Artifact `previous_id` 表示前一版本记录引用；Event 使用 type/subject/time/actor/related/content/reply_to，回复关联验证在 M1.3 实现。
