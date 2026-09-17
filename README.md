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
| M0.3 文本存储基元 | 未开始 |

## 构建与验收

```sh
go build -o bin/devsys.exe ./cmd/devsys
bin/devsys.exe --version      # devsys 0.1.0-dev
bin/devsys.exe --help
bin/devsys.exe bogus; echo $? # 2（用法错误）

# 在 Git 仓库根目录初始化项目状态目录（幂等；重复执行不覆盖已有内容）
bin/devsys.exe init
bin/devsys.exe init --json    # 机器可读输出
bin/devsys.exe init; echo $?  # 非 Git 目录：3（前置条件错误）
```

`init` 创建 `.devsys/`（方案 §14.3，另含 `.devsys/.gitignore` 排除 `local/` 与 `.cache/`），
并在用户级注册表（`DEVSYS_CONFIG_DIR` 可覆盖）登记项目路径。

带版本信息构建（发布用）：

```sh
go build -ldflags "-X workloom/internal/version.Version=0.1.0 -X workloom/internal/version.Commit=$(git rev-parse --short HEAD)" -o bin/devsys.exe ./cmd/devsys
```

## 目录结构

```
cmd/devsys/          命令入口（进程退出码）
internal/cli/        命令分发、全局开关、输出与退出码
internal/project/    init 与项目内布局（后续：存储 / 领域）
internal/registry/   用户级项目路径注册表
internal/minyaml/    M0.2 占位序列化（M0.3 起替换为存储基元）
internal/version/    构建标识（可用 -ldflags 覆盖）
docs/原始文档/       方案与实施计划（源文档，不再拆分）
bin/                 构建产物（不提交）
```

后续面向使用者的手册与迁移说明放在 `docs/` 下（不进 `docs/原始文档/`）。

## 约定

- 核心用 Go；M7 的工作区视图用 TypeScript/React 实现，只读渲染、不碰进程。
- 一切项目状态写成可 diff、可合并的文本（YAML / Markdown / JSONL）。
- 不使用数据库；任何缓存都是可删除、可重建的普通文件。
- 行尾统一 LF（见 `.gitattributes`），保证状态文件跨机器合并不产生幽灵 diff。
- 时间估算不进入状态输出（见方案 §7.4）。
