// Package sitestatic renders a view.Model as an offline static site (实施计划
// M7.2, 方案 §17 两种形态之一）。It is a pure renderer: the model comes from
// internal/view (the only data entry point, read-only under the shared lock),
// and the only writes are the site files below the output directory.
//
// The site is self-contained on purpose (验收：断网可看）: no JavaScript, no
// external references — one local stylesheet, relative links only.
package sitestatic

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/next"
	"github.com/JAYY513/Workloom/internal/view"
)

// PageFiles are the site pages in navigation order.
var PageFiles = []string{
	"index.html",
	"tasks.html",
	"workflows.html",
	"runs.html",
	"records.html",
	"knowledge.html",
}

// DefaultOutRel is the default output directory, relative to the project root.
// It lives under .devsys/dist/ (git-ignored local derivation): generated HTML
// must not pollute git freshness signals (knowledge.Changes counts untracked
// files), so the default is never a committable location. Point --out at a
// tracked directory explicitly to publish the site.
const DefaultOutRel = ".devsys/dist/site"

// DefaultOutDir resolves the default output directory for root.
func DefaultOutDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(DefaultOutRel))
}

// ResolveOut validates --out against root and returns the absolute output
// directory. Empty selects the default. Relative paths resolve against root;
// absolute paths (including ones outside the project) are accepted as-is.
//
// Refused (usage errors, the caller maps them onto code 2): the .devsys
// directory itself or anything below it except .devsys/dist/ (state must not
// be overwritten), the project root itself, and anything under .git.
func ResolveOut(root, raw string) (string, error) {
	abs := DefaultOutDir(root)
	if strings.TrimSpace(raw) != "" {
		if filepath.IsAbs(raw) {
			abs = filepath.Clean(raw)
		} else {
			abs = filepath.Join(root, filepath.FromSlash(raw))
		}
	} else {
		abs = filepath.Clean(abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		// Different volume (Windows) or otherwise unrelated: an explicit
		// outside directory, allowed.
		return abs, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// Explicit escape from the project (e.g. --out ../publish): allowed,
		// exactly like an absolute outside directory. It is never confused
		// with managed state below.
		return abs, nil
	}
	lower := strings.ToLower(rel)
	sep := string(filepath.Separator)
	switch {
	case rel == ".":
		return "", fmt.Errorf("refusing to build into the project root itself")
	case lower == ".git" || strings.HasPrefix(lower, ".git"+sep):
		return "", fmt.Errorf("refusing to build into .git (%q)", raw)
	case lower == ".devsys" || (strings.HasPrefix(lower, ".devsys"+sep) &&
		lower != ".devsys"+sep+"dist" && !strings.HasPrefix(lower, ".devsys"+sep+"dist"+sep)):
		return "", fmt.Errorf("refusing to build into managed state %q (only .devsys/dist/ is writable output)", raw)
	}
	return abs, nil
}

// kv is one sorted map entry; template map iteration order is random, so every
// map the site renders is pre-sorted in Go.
type kv struct {
	K string
	V int
}

func sortedCounts(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// workflowGroup is one workflow's items, in item-ID order (the model's order).
type workflowGroup struct {
	ID    string
	Items []view.Item
}

func groupWorkflows(items []view.Item) []workflowGroup {
	var groups []workflowGroup
	index := map[string]int{}
	for _, it := range items {
		if it.Workflow == "" {
			continue
		}
		i, ok := index[it.Workflow]
		if !ok {
			i = len(groups)
			index[it.Workflow] = i
			groups = append(groups, workflowGroup{ID: it.Workflow})
		}
		groups[i].Items = append(groups[i].Items, it)
	}
	return groups
}

// siteData is the template input for every page.
type siteData struct {
	M *view.Model
	// Title/Active drive <title> and the nav highlight.
	Title  string
	Active string
	// Section names the page's own provenance block.
	Section string
	Sources []string
	// GeneratedAt is the snapshot time (UTC, second precision).
	GeneratedAt string
	// Trust rendering (no template logic on raw state strings).
	ShowBanner bool
	IsPending  bool
	TrustState string
	TrustNote  string
	Pending    []string
	// Freshness rendering (derived in Go; the templates only switch on
	// ShowFreshness). FreshnessCmd is empty when no command applies
	// (fresh, or unavailable where refresh would point at nothing to fix).
	ShowFreshness   bool
	FreshnessTitle  string
	FreshnessText   string
	FreshnessReason string
	FreshnessCmd    string
	// Derived, order-stable.
	ProgressCounts   []kv
	RecentRuns       []view.RunEntry
	RecentDecisions  []view.RecordRef
	RecentArtifacts  []view.RecordRef
	WorkflowGroups   []workflowGroup
	PendingApprovals []next.Risk
}

func head5[T any](list []T) []T {
	if len(list) > 5 {
		return list[:5]
	}
	return list
}

// freshnessHint derives the site-wide freshness banner from the knowledge
// status (M7.4, 方案 §17 数据过期时明确提示）. The model is fact; this is
// rendering — the caller keeps the verdict strings out of the templates.
func freshnessHint(k view.Knowledge) (show bool, title, text, reason, cmd string) {
	switch k.Status {
	case view.KnowledgeStale:
		return true, "知识已过期", "执行以下命令重新生成受影响页面：", k.Reason, "workloom knowledge refresh"
	case view.KnowledgeMissing:
		return true, "知识页面层缺失", "配置 knowledge_generator 后执行：", k.Reason, "workloom knowledge refresh --full"
	case view.KnowledgeUnavailable:
		return true, "知识新鲜度不可判", "", k.Reason, ""
	default:
		return false, "", "", "", ""
	}
}

func newSiteData(m *view.Model, generatedAt time.Time, title, active, section string, sources []string) siteData {
	var approvals []next.Risk
	for _, r := range m.Progress.Readiness.Risks {
		if r.Kind == next.RiskPendingApproval {
			approvals = append(approvals, r)
		}
	}
	show, ftitle, ftext, freason, fcmd := freshnessHint(m.Knowledge)
	return siteData{
		M: m, Title: title, Active: active, Section: section, Sources: sources,
		GeneratedAt:      generatedAt.UTC().Format(time.RFC3339),
		ShowBanner:       m.Trust.State != view.TrustOK,
		IsPending:        m.Trust.State == view.TrustPending,
		TrustState:       m.Trust.State,
		TrustNote:        m.Trust.Note,
		Pending:          m.Trust.Pending,
		ShowFreshness:    show,
		FreshnessTitle:   ftitle,
		FreshnessText:    ftext,
		FreshnessReason:  freason,
		FreshnessCmd:     fcmd,
		ProgressCounts:   sortedCounts(m.Progress.Counts),
		RecentRuns:       head5(m.Runs.Entries),
		RecentDecisions:  head5(m.Records.Decisions),
		RecentArtifacts:  head5(m.Records.Artifacts),
		WorkflowGroups:   groupWorkflows(m.Progress.Items),
		PendingApprovals: approvals,
	}
}

// short12 abbreviates a commit the way the view's human face does (12 chars).
func short12(commit string) string {
	if commit == "" {
		return "(none)"
	}
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

// ftime renders model times; the zero value and nil stay an em dash.
func ftime(v any) string {
	const layout = "2006-01-02 15:04:05Z"
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return "—"
		}
		return t.UTC().Format(layout)
	case *time.Time:
		if t == nil || t.IsZero() {
			return "—"
		}
		return t.UTC().Format(layout)
	default:
		return fmt.Sprint(v)
	}
}

func dirtyStr(dirty *bool, changed int) string {
	if dirty == nil {
		return "unknown"
	}
	if *dirty {
		return fmt.Sprintf("yes (%d file(s))", changed)
	}
	return "no"
}

var siteFuncs = template.FuncMap{
	"short12":  short12,
	"ftime":    ftime,
	"dirtyStr": dirtyStr,
	"join":     strings.Join,
}

// styleCSS is the whole site stylesheet: hand-written, no @import, no url(),
// no external references of any kind.
const styleCSS = `:root{color-scheme:light}
body{font-family:system-ui,-apple-system,"Segoe UI","Noto Sans SC",sans-serif;margin:0 auto;max-width:1024px;padding:0 16px 64px;color:#1a1a1a;background:#fff;line-height:1.6}
nav.top{position:sticky;top:0;background:#fff;border-bottom:2px solid #1a1a1a;padding:8px 0;display:flex;gap:4px;flex-wrap:wrap}
nav.top a{padding:4px 10px;text-decoration:none;color:#1a1a1a;border:1px solid transparent;border-radius:4px}
nav.top a.active{background:#1a1a1a;color:#fff}
nav.top a:hover{border-color:#1a1a1a}
.banner{background:#fff4e5;border:1px solid #e8a13d;padding:8px 12px;margin:12px 0;border-radius:6px}
h1{font-size:24px}h2{font-size:19px;margin-top:28px;border-bottom:1px solid #ddd;padding-bottom:4px}h3{font-size:16px}
table{border-collapse:collapse;width:100%;margin:8px 0;font-size:14px}
th,td{border:1px solid #ddd;padding:4px 8px;text-align:left;vertical-align:top}
th{background:#f4f4f4;white-space:nowrap}
code{background:#f4f4f4;padding:1px 5px;border-radius:3px;font-size:13px}
footer{margin-top:36px;border-top:2px solid #1a1a1a;padding-top:8px;font-size:13px;color:#444}
footer ul{margin:4px 0;padding-left:20px}
.empty{color:#888}
.verdict{font-weight:bold}
ul.tight{margin:4px 0;padding-left:20px}
p.meta{color:#444;font-size:14px}
`

const siteTemplates = `{{define "head"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — 工作区视图</title>
<link rel="stylesheet" href="assets/style.css">
</head>
<body>
{{template "nav" .}}
{{template "banner" .}}
{{template "freshness" .}}
<main>
{{end}}
{{define "freshness"}}{{if .ShowFreshness}}<div class="banner" role="alert"><strong>{{.FreshnessTitle}}</strong>{{if .FreshnessReason}} — {{.FreshnessReason}}{{end}}{{if .FreshnessText}}<br>{{.FreshnessText}}{{if .FreshnessCmd}}<code>{{.FreshnessCmd}}</code>{{end}}{{end}}</div>{{end}}{{end}}
{{define "nav"}}<nav class="top">
<a href="index.html"{{if eq .Active "index.html"}} class="active"{{end}}>总览</a>
<a href="tasks.html"{{if eq .Active "tasks.html"}} class="active"{{end}}>任务</a>
<a href="workflows.html"{{if eq .Active "workflows.html"}} class="active"{{end}}>工作流</a>
<a href="runs.html"{{if eq .Active "runs.html"}} class="active"{{end}}>运行</a>
<a href="records.html"{{if eq .Active "records.html"}} class="active"{{end}}>记录</a>
<a href="knowledge.html"{{if eq .Active "knowledge.html"}} class="active"{{end}}>知识</a>
</nav>{{end}}
{{define "banner"}}{{if .ShowBanner}}<div class="banner" role="alert"><strong>trust: {{.TrustState}}</strong>{{if .TrustNote}} — {{.TrustNote}}{{end}}{{if .Pending}}<br>pending: {{join .Pending ", "}}{{end}}{{if .IsPending}}<br>存在未完成的事务，业务数据按方案 §15.4 置空：先执行 <code>workloom recover</code> 再信任本页。可用 <code>workloom doctor</code> 先查看事务详情。{{end}}</div>{{end}}{{end}}
{{define "foot"}}</main>
<footer>
<h3>数据来源与基线</h3>
<p class="meta">本节来源文件：{{if .Sources}}{{join .Sources ", "}}{{else}}（无）{{end}}</p>
<p class="meta">全站来源文件见 <a href="data/model.json">data/model.json</a>（与 <code>workloom workspace view --json</code> 同一模型）。</p>
<p class="meta">基线提交：{{if .M.Baseline.Available}}<code>{{.M.Baseline.Commit}}</code>（{{.M.Baseline.Branch}}）· 工作区脏：{{dirtyStr .M.Baseline.Dirty .M.Baseline.ChangedFiles}}{{else}}不可用{{if .M.Baseline.Reason}} — {{.M.Baseline.Reason}}{{end}}{{end}} · 生成时间：{{.GeneratedAt}}</p>
</footer>
</body>
</html>{{end}}
{{define "readiness"}}<h2>就绪判定（§7.4）</h2>
<p>结论：<span class="verdict">{{.M.Progress.Readiness.Verdict}}</span></p>
{{if .M.Progress.Readiness.Reasons}}<ul class="tight">{{range .M.Progress.Readiness.Reasons}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Progress.Readiness.Risks}}<h3>风险</h3>
<table><tr><th>种类</th><th>任务</th><th>说明</th></tr>
{{range .M.Progress.Readiness.Risks}}<tr><td><code>{{.Kind}}</code></td><td>{{.WorkitemID}}</td><td>{{.Detail}}</td></tr>{{end}}</table>{{end}}
{{if .M.Progress.Readiness.Fixes}}<h3>修复</h3>
<table><tr><th>原因</th><th>命令</th></tr>
{{range .M.Progress.Readiness.Fixes}}<tr><td>{{.Reason}}</td><td><code>{{.Command}}</code></td></tr>{{end}}</table>{{end}}
<p>推荐下一步：<code>{{.M.Progress.Readiness.Next.Action}}</code>{{if .M.Progress.Readiness.Next.WorkitemID}} {{.M.Progress.Readiness.Next.WorkitemID}}{{end}}{{if .M.Progress.Readiness.Next.MilestoneID}} {{.M.Progress.Readiness.Next.MilestoneID}}{{end}} — {{.M.Progress.Readiness.Next.Reason}}</p>{{end}}
{{define "index"}}{{template "head" .}}
<h1>项目总览</h1>
<p class="meta">{{.M.Project.Name}}（{{.M.Project.ID}}）— {{.M.Project.Status}}{{if .M.Project.CurrentPhase}} · 当前阶段：{{.M.Project.CurrentPhase}}{{end}}</p>
{{if .M.Project.Description}}<p>{{.M.Project.Description}}</p>{{end}}
{{if .M.Project.Summary}}<p>当前状态：{{.M.Project.Summary}}</p>{{end}}
<h2>蓝图与目标</h2>
{{if .M.Project.Goals}}<h3>目标</h3><ul class="tight">{{range .M.Project.Goals}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Project.Scope.In}}<h3>范围内</h3><ul class="tight">{{range .M.Project.Scope.In}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Project.Scope.Out}}<h3>范围外</h3><ul class="tight">{{range .M.Project.Scope.Out}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Project.Constraints}}<h3>约束</h3><ul class="tight">{{range .M.Project.Constraints}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Project.TechStack}}<h3>技术栈</h3><p>{{join .M.Project.TechStack ", "}}</p>{{end}}
<h2>里程碑</h2>
{{if .M.Project.Milestones}}<table><tr><th>ID</th><th>名称</th><th>状态</th></tr>
{{range .M.Project.Milestones}}<tr><td><code>{{.ID}}</code></td><td>{{.Name}}</td><td>{{.Status}}</td></tr>{{end}}</table>{{else}}<p class="empty">无里程碑</p>{{end}}
<h2>进度</h2>
<p>{{len .M.Progress.Items}} 个任务 — {{range $i, $c := .ProgressCounts}}{{if $i}} · {{end}}{{$c.K}} {{$c.V}}{{end}}</p>
{{if .M.Project.Blockers}}<h3>阻塞</h3><ul class="tight">{{range .M.Project.Blockers}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Project.Risks}}<h3>风险</h3><ul class="tight">{{range .M.Project.Risks}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Project.NextFocus}}<h3>下一步重点</h3><ul class="tight">{{range .M.Project.NextFocus}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{template "readiness" .}}
<h2>最近活动</h2>
<h3>运行</h3>
{{if .RecentRuns}}<table><tr><th>ID</th><th>任务</th><th>状态</th><th>开始</th><th>结论</th></tr>
{{range .RecentRuns}}<tr><td><code>{{.ID}}</code></td><td>{{.WorkitemID}}</td><td>{{.Status}}</td><td>{{ftime .StartedAt}}</td><td>{{.Summary}}</td></tr>{{end}}</table>{{else}}<p class="empty">无运行记录</p>{{end}}
<h3>决策</h3>
{{if .RecentDecisions}}<ul class="tight">{{range .RecentDecisions}}<li><code>{{.ID}}</code> {{.Title}}</li>{{end}}</ul>{{else}}<p class="empty">无决策记录</p>{{end}}
<h3>产物</h3>
{{if .RecentArtifacts}}<ul class="tight">{{range .RecentArtifacts}}<li><code>{{.ID}}</code> {{.Title}}</li>{{end}}</ul>{{else}}<p class="empty">无产物记录</p>{{end}}
{{template "foot" .}}{{end}}
{{define "tasks"}}{{template "head" .}}
<h1>任务看板</h1>
<p class="meta">{{len .M.Progress.Items}} 个任务</p>
<h2>阶段分布</h2>
<table><tr><th>状态</th><th>计数</th></tr>
{{range .ProgressCounts}}<tr><td>{{.K}}</td><td>{{.V}}</td></tr>{{end}}</table>
<h2>任务表</h2>
{{if .M.Progress.Items}}<table><tr><th>ID</th><th>标题</th><th>类型</th><th>状态</th><th>优先级</th><th>工作流</th><th>步骤</th><th>负责人</th><th>依赖</th></tr>
{{range .M.Progress.Items}}<tr><td><code>{{.ID}}</code></td><td>{{.Title}}</td><td>{{.Type}}</td><td>{{.Status}}</td><td>{{.Priority}}</td><td>{{.Workflow}}</td><td>{{.WorkflowStep}}{{if .WorkflowPaused}}（已暂停）{{end}}</td><td>{{.AssignedAgent}}{{if .AssignedHarness}}/{{.AssignedHarness}}{{end}}</td><td>{{join .Dependencies ", "}}</td></tr>{{end}}</table>{{else}}<p class="empty">无任务</p>{{end}}
<h2>依赖关系</h2>
{{if .M.Progress.Items}}<ul class="tight">{{range .M.Progress.Items}}{{if .Dependencies}}<li><code>{{.ID}}</code> 依赖 {{join .Dependencies ", "}}</li>{{end}}{{end}}</ul>{{end}}
{{template "readiness" .}}
{{template "foot" .}}{{end}}
{{define "workflows"}}{{template "head" .}}
<h1>工作流</h1>
{{if .WorkflowGroups}}{{range .WorkflowGroups}}<h2>{{.ID}}</h2>
<table><tr><th>任务</th><th>标题</th><th>当前步骤</th><th>状态</th><th>负责人</th><th>租约</th></tr>
{{range .Items}}<tr><td><code>{{.ID}}</code></td><td>{{.Title}}</td><td>{{.WorkflowStep}}{{if .WorkflowPaused}}（已暂停）{{end}}</td><td>{{.Status}}{{if .SchedulingState}}/{{.SchedulingState}}{{end}}</td><td>{{.AssignedAgent}}</td><td>{{.LeaseOwner}} {{ftime .LeaseUntil}}</td></tr>{{end}}</table>{{end}}{{else}}<p class="empty">暂无任务绑定工作流</p>{{end}}
<h2>待审批</h2>
{{if .PendingApprovals}}<table><tr><th>种类</th><th>任务</th><th>说明</th></tr>
{{range .PendingApprovals}}<tr><td><code>{{.Kind}}</code></td><td>{{.WorkitemID}}</td><td>{{.Detail}}</td></tr>{{end}}</table>{{else}}<p class="empty">无待审批事项</p>{{end}}
{{template "foot" .}}{{end}}
{{define "runs"}}{{template "head" .}}
<h1>运行时间线</h1>
<p class="meta">{{len .M.Runs.Entries}} 条记录（按开始时间倒序）{{if .M.Runs.Truncated}} — 已截断，提高 --limit 查看更多{{end}}</p>
{{if .M.Runs.Entries}}<table><tr><th>ID</th><th>任务</th><th>状态</th><th>阶段</th><th>Agent</th><th>Harness</th><th>模型</th><th>工作区</th><th>分支</th><th>尝试</th><th>开始</th><th>结束</th><th>结论</th><th>错误</th><th>备注</th></tr>
{{range .M.Runs.Entries}}<tr><td><code>{{.ID}}</code></td><td>{{.WorkitemID}}</td><td>{{.Status}}</td><td>{{.Phase}}</td><td>{{.AgentID}}</td><td>{{.Harness}}</td><td>{{.Model}}</td><td>{{.Workspace}}</td><td>{{.Branch}}</td><td>{{.Attempt}}{{if .RetryCount}}（重试 {{.RetryCount}}）{{end}}</td><td>{{ftime .StartedAt}}</td><td>{{ftime .FinishedAt}}</td><td>{{.Summary}}</td><td>{{join .Errors "; "}}</td><td>{{if .StreamTorn}}事件流断尾{{end}}</td></tr>{{end}}</table>{{else}}<p class="empty">无运行记录</p>{{end}}
{{template "foot" .}}{{end}}
{{define "records"}}{{template "head" .}}
<h1>记录</h1>
<h2>决策（{{len .M.Records.Decisions}}）</h2>
{{if .M.Records.Decisions}}<table><tr><th>ID</th><th>标题</th><th>状态</th><th>关联任务</th><th>创建</th></tr>
{{range .M.Records.Decisions}}<tr><td><code>{{.ID}}</code></td><td>{{.Title}}</td><td>{{.Status}}</td><td>{{join .RelatedWorkItems ", "}}</td><td>{{ftime .CreatedAt}}</td></tr>{{end}}</table>{{else}}<p class="empty">无决策记录</p>{{end}}
<h2>发现（{{len .M.Records.Findings}}）</h2>
{{if .M.Records.Findings}}<table><tr><th>ID</th><th>标题</th><th>级别</th><th>关联任务</th></tr>
{{range .M.Records.Findings}}<tr><td><code>{{.ID}}</code></td><td>{{.Title}}</td><td>{{.Severity}}</td><td>{{join .RelatedWorkItems ", "}}</td></tr>{{end}}</table>{{else}}<p class="empty">无发现记录</p>{{end}}
<h2>产物（{{len .M.Records.Artifacts}}）</h2>
{{if .M.Records.Artifacts}}<table><tr><th>ID</th><th>标题</th><th>版本</th><th>关联任务</th><th>创建</th></tr>
{{range .M.Records.Artifacts}}<tr><td><code>{{.ID}}</code></td><td>{{.Title}}</td><td>{{.Version}}</td><td>{{join .RelatedWorkItems ", "}}</td><td>{{ftime .CreatedAt}}</td></tr>{{end}}</table>{{else}}<p class="empty">无产物记录</p>{{end}}
{{if .M.Records.Truncated}}<p class="meta">列表已截断，提高 --limit 查看更多。</p>{{end}}
{{template "foot" .}}{{end}}
{{define "knowledge"}}{{template "head" .}}
<h1>知识新鲜度</h1>
<p>状态：<span class="verdict">{{.M.Knowledge.Status}}</span>{{if .M.Knowledge.Reason}} — {{.M.Knowledge.Reason}}{{end}}</p>
{{if .ShowFreshness}}{{if .FreshnessText}}<p>{{.FreshnessText}}{{if .FreshnessCmd}}<code>{{.FreshnessCmd}}</code>{{end}}</p>{{end}}{{end}}
<ul class="tight">
<li>基线：{{if .M.Knowledge.Baseline}}<code>{{short12 .M.Knowledge.Baseline}}</code>{{else}}（无）{{end}} · 当前：{{if .M.Knowledge.Head}}<code>{{short12 .M.Knowledge.Head}}</code>{{else}}（无）{{end}}{{if .M.Knowledge.Branch}}（{{.M.Knowledge.Branch}}）{{end}}</li>
<li>索引：{{if .M.Knowledge.IndexReady}}就绪（{{.M.Knowledge.IndexFiles}} 文件）{{else}}未就绪{{end}}{{if .M.Knowledge.Generator}} · 生成器：{{.M.Knowledge.Generator}}{{end}}{{if .M.Knowledge.Adapter}}（{{.M.Knowledge.Adapter}}）{{end}}</li>
<li>变更文件：{{.M.Knowledge.ChangedFiles}} · 受影响页面：{{len .M.Knowledge.Affected}}</li>
</ul>
{{if .M.Knowledge.Affected}}<h2>受影响页面</h2><ul class="tight">{{range .M.Knowledge.Affected}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .M.Knowledge.Unverifiable}}<h2>无法验证</h2><ul class="tight">{{range .M.Knowledge.Unverifiable}}<li>{{.}}</li>{{end}}</ul>{{end}}
<h2>页面（{{len .M.Knowledge.Pages}}）</h2>
{{if .M.Knowledge.Pages}}<table><tr><th>路径</th><th>类型</th><th>状态</th><th>来源提交</th><th>过期</th><th>原因</th><th>相关任务</th></tr>
{{range .M.Knowledge.Pages}}<tr><td>{{.Path}}</td><td>{{.Type}}</td><td>{{.Status}}</td><td>{{if .SourceCommit}}<code>{{short12 .SourceCommit}}</code>{{end}}</td><td>{{if .Stale}}是{{else}}否{{end}}</td><td>{{.Reason}}</td><td>{{join .RelatedWorkItems ", "}}</td></tr>{{end}}</table>{{else}}<p class="empty">无页面</p>{{end}}
{{if .M.Knowledge.Truncated}}<p class="meta">列表已截断，提高 --limit 查看更多。</p>{{end}}
{{template "foot" .}}{{end}}`

var siteTmpl = template.Must(template.New("site").Funcs(siteFuncs).Parse(siteTemplates))

// pageMeta binds a page file to its title and provenance.
type pageMeta struct {
	file    string
	tmpl    string
	title   string
	section string
	sources []string
}

func pageMetas(m *view.Model) []pageMeta {
	return []pageMeta{
		{"index.html", "index", "总览", "项目蓝图与目标", m.Project.Sources},
		{"tasks.html", "tasks", "任务看板", "进度（任务看板与依赖）", m.Progress.Sources},
		{"workflows.html", "workflows", "工作流", "进度（工作流实例）", m.Progress.Sources},
		{"runs.html", "runs", "运行时间线", "执行（Run 时间线与产物）", m.Runs.Sources},
		{"records.html", "records", "记录", "记录（决策/发现/产物）", m.Records.Sources},
		{"knowledge.html", "knowledge", "知识新鲜度", "知识（模块索引与新鲜度）", m.Knowledge.Sources},
	}
}

// Build renders the whole site below out (created with MkdirAll, including
// assets/ and data/) and returns the page files. generatedAt stamps the page
// footers only — data/model.json carries the model verbatim, so two builds of
// the same state with the same generatedAt are byte-identical.
func Build(m *view.Model, out string, generatedAt time.Time) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("sitestatic: nil model")
	}
	if err := os.MkdirAll(filepath.Join(out, "assets"), 0o755); err != nil {
		return nil, fmt.Errorf("sitestatic: mkdir assets: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(out, "data"), 0o755); err != nil {
		return nil, fmt.Errorf("sitestatic: mkdir data: %w", err)
	}
	modelJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("sitestatic: encode model: %w", err)
	}
	modelJSON = append(modelJSON, '\n')
	files := map[string][]byte{
		filepath.Join("assets", "style.css"): []byte(styleCSS),
		filepath.Join("data", "model.json"):  modelJSON,
	}
	metas := pageMetas(m)
	pages := make([]string, 0, len(metas))
	for _, pm := range metas {
		var sb strings.Builder
		data := newSiteData(m, generatedAt, pm.title, pm.file, pm.section, pm.sources)
		if err := siteTmpl.ExecuteTemplate(&sb, pm.tmpl, data); err != nil {
			return nil, fmt.Errorf("sitestatic: render %s: %w", pm.file, err)
		}
		files[pm.file] = []byte(sb.String())
		pages = append(pages, pm.file)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(out, filepath.FromSlash(name)), files[name], 0o644); err != nil {
			return nil, fmt.Errorf("sitestatic: write %s: %w", name, err)
		}
	}
	return pages, nil
}
