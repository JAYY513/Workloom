# Contrabass 作为可选执行后端（M8.4，方案 §9.3/§10.3）

本目录验证「可选后端」路径：devsys 工作项映射到看板卡片、在看板侧执行、
执行结论经 devsys CLI 回流。**`.devsys/` 是唯一事实来源**：看板卡片是投递
副本，不是第二来源；回流只调 CLI，不直写状态文件。

Windows 用 `py -3` 显式调用（`python3` 别名在本机指向商店占位）；
POSIX 可直接执行（shebang `#!/usr/bin/env python3`）。仅依赖 Python 标准库。

## 命令

```sh
# workloom -> board（幂等，重跑同字节）
py -3 scripts/contrabass/export.py --root <项目根> --out <board目录> [--id WLM-1,...] [--devsys /path/to/workloom]
# board -> workloom（先预览，再回流）
py -3 scripts/contrabass/import.py --root <项目根> --board <board目录> --dry-run --actor <人> --reason <原因>
py -3 scripts/contrabass/import.py --root <项目根> --board <board目录> --actor <人> --reason <原因>
# 空 board 骨架（无 Contrabass 环境时的仿真起点）
py -3 scripts/contrabass/export.py --root <项目根> --out <board目录> --init-fixture
```

## 卡片形态（中间形态，非 Contrabass 原生）

`<board>/issues/<WLM-id>.json`：

```json
{
  "key": "WLM-1",
  "title": "…",
  "description": "…",
  "devsys_status": "ready",
  "board_status": "open",
  "labels": [],
  "priority": 5,
  "acceptance_criteria": ["…"],
  "source": {"project": "demo", "workitem_version": "<64-hex>"},
  "devsys_updated_at": "2026-09-19T…Z",
  "result": null
}
```

`result` 由看板侧执行后填写：`{"summary": "结论一句话", "log": "证据摘录"}`；
import 把 `summary` 搬进 `workitem comment`（`[board] ` 前缀）。

## 映射表

devsys 九态 → board 六态（export；其中 open/review 为合流态）：

| devsys | board | 备注 |
|---|---|---|
| draft/backlog/ready | open | 未开始 |
| retry_queued | open + `retry-queued` label | 等待重试 |
| in_progress | in_progress | 执行中 |
| review/verification | review | 合流：board 不区分两级复核 |
| done | done | 终态 |
| blocked | blocked | 阻塞 |
| cancelled | closed | 终态 |

board → devsys（import，保守子集）：仅 `done/review/blocked/closed→cancelled`
四种做 `transition`（带 `--expect` 版本守卫，非法边/过期版本进 `refused`）；
`open/in_progress` 与任何未知 board 状态同样只搬 `result.summary` 为 comment，不动状态。

priority 数值原样透传（board 若只认高中低，由看板侧分档，本脚本不猜）。

## 差异记录

- `[UNVERIFIED]` board 字段假设：真实 Contrabass 的 `.contrabass/board/`
  原生字段形态未实测（本机无 Contrabass，外网不可达）。本脚本读写的是上述
  中间形态；联调真 Contrabass 前先对拍字段，再写一层薄转换。
- 九态→六态多对一不可逆：`review/verification` 合流后回流只能到 `review`；
  `retry_queued` 回流不恢复重试排期，只留 comment。需要精确恢复时走 devsys
  原生命令，不走 board。
- `runs/*.yaml` 摘要与 run 流不进 board：执行证据以 comment + 事件形式回流，
  运行级证据仍在 devsys 侧（`run log`）。
- board 侧新增字段一律忽略（只读 `key/board_status/result`）。
- 无实时同步：M8 仅设备接力；board 副本的新鲜度由操作者保证（export 即快照）。

## 复验（fixture 闭环，本机已跑通）

```sh
export DEVSYS_CONFIG_DIR=/tmp/cfg
# 1. 建任务并走到可 done 的前一态（board done→devsys done 要求合法边）
workloom workitem transition --id WLM-1 --to backlog|ready|in_progress|review|verification …
# 2. export → 改卡片 board_status=done + 填 result → import --dry-run（零写）→ import
# 3. 断言：workitem get 为 done，comment 落定，event list 有 transition+comment 痕迹
```

真机复验（有 Contrabass 后）：把 `--out` 指向其 board 目录（或按其字段写
转换层），跑同一闭环；`workitem get` + `event list` 为验收证据。
