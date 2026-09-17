# Workloom

独立于 Harness 的 Agent 开发基础设施。状态保存在项目仓库内（`.devsys/`），
通过 Git 在设备之间同步，不依赖跨项目共享数据库。

- 设计与选型：[`独立于Harness的Agent开发基础设施方案.md`](独立于Harness的Agent开发基础设施方案.md)
- 实施步骤与验收：[`实施计划.md`](实施计划.md)

## 当前进度

| 里程碑 | 状态 |
|---|---|
| M0.1 语言与工程骨架 | 已完成（`devsys --version` 可跑） |
| M0.2 起 | 未开始 |

## 构建与验收

```sh
go build -o bin/devsys.exe ./cmd/devsys
bin/devsys.exe --version      # devsys 0.1.0-dev
bin/devsys.exe --help
bin/devsys.exe bogus; echo $? # 2
```

带版本信息构建（发布用）：

```sh
go build -ldflags "-X workloom/internal/version.Version=0.1.0 -X workloom/internal/version.Commit=$(git rev-parse --short HEAD)" -o bin/devsys.exe ./cmd/devsys
```

## 目录结构

```
cmd/devsys/          命令入口（参数分发、全局开关、退出码）
internal/            内部包（后续：storage / domain / access / execute）
internal/version/    构建标识（可用 -ldflags 覆盖）
bin/                 构建产物（不提交）
```

## 约定

- 核心用 Go；M7 的工作区视图用 TypeScript/React 实现，只读渲染、不碰进程。
- 一切项目状态写成可 diff、可合并的文本（YAML / Markdown / JSONL）。
- 不使用数据库；任何缓存都是可删除、可重建的普通文件。
- 时间估算不进入状态输出（见方案 §7.4）。
