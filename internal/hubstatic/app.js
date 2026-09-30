(() => {
"use strict";

// ---- state ---------------------------------------------------------------
const state = {
  projects: [],          // GET /api/projects items
  id: "",                // selected project id ("" = none / overview)
  section: "dashboard",  // dashboard | blueprint | state | task | flows | approvals | records | runs | knowledge
  task: "",              // selected task id
  model: null,           // current view model
  lastGood: {},          // id -> last successful model
  etag: {},              // url -> etag
  refreshError: "",
  lastOkAt: null,
  addError: "",
  addBusy: false,
  started: false,
};
const $ = (s) => document.querySelector(s);
const main = () => $("#main");

// ---- escaping-safe rendering ---------------------------------------------
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const encID = (v) => encodeURIComponent(v);

// ---- status mapping --------------------------------------------------------
// domain statuses -> dot tone + display label
const STATUS_TONE = {
  "进行中": "st-run", "in_progress": "st-run",
  "评审": "st-wait", "review": "st-wait",
  "验证": "st-wait", "verify": "st-wait", "verification": "st-wait",
  "待办": "", "todo": "", "backlog": "", "draft": "",
  "就绪": "st-ok", "ready": "st-ok",
  "重试排队": "st-wait", "retry_queued": "st-wait",
  "阻塞": "st-bad", "blocked": "st-bad",
  "失败": "st-bad", "failed": "st-bad",
  "完成": "st-ok", "done": "st-ok",
  "取消": "", "cancelled": "", "canceled": "",
  "申请中": "st-wait",
  "已批准": "st-ok", "approved": "st-ok",
  "已拒绝": "st-bad", "rejected": "st-bad",
  "通过": "st-ok", "pass": "st-ok",
  "进行中项目": "st-run", "active": "st-run",
};
const STATUS_LABEL = {
  "in_progress": "进行中", "review": "评审", "verify": "验证", "verification": "验证",
  "todo": "待办", "backlog": "待办", "draft": "草稿", "ready": "就绪",
  "retry_queued": "重试排队",
  "blocked": "阻塞", "failed": "失败", "done": "完成",
  "cancelled": "取消", "canceled": "取消",
  "pass": "通过", "approved": "已批准", "rejected": "已拒绝",
  "active": "进行中",
};
function statusLabel(s) { return STATUS_LABEL[s] || s || "—"; }
function statusDot(s) {
  const label = statusLabel(s);
  return `<span class="status ${STATUS_TONE[s] || ""}">${esc(label)}</span>`;
}
const K_LABEL = { fresh: "新鲜", stale: "过期", missing: "没有", unavailable: "判断不了" };
const K_DOT = { fresh: "ok", stale: "warn", missing: "idle", unavailable: "bad" };
function knowledgeLabel(s) { return K_LABEL[s] || s || "—"; }

// ---- api -------------------------------------------------------------------
async function apiGet(url) {
  const headers = { Accept: "application/json" };
  if (state.etag[url]) headers["If-None-Match"] = state.etag[url];
  const r = await fetch(url, { headers });
  if (r.status === 304) return { notModified: true };
  if (!r.ok) throw new Error((await r.text()) || ("HTTP " + r.status));
  const tag = r.headers.get("ETag");
  if (tag) state.etag[url] = tag;
  return { body: await r.json() };
}
async function apiPost(url, payload) {
  const r = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await r.json().catch(() => null);
  if (!r.ok) throw new Error((body && body.error) || ("HTTP " + r.status));
  return body;
}

// ---- derived model helpers -------------------------------------------------
function d() { return state.model || {}; }
function items() { return (d().progress && d().progress.items) || []; }
function item(id) { return items().find((it) => it.id === id); }
function missing() {
  const p = state.projects.find((x) => x.id === state.id);
  return !!(p && !p.exists);
}
function pending() { return d().trust && d().trust.state === "pending_transaction"; }
function advisory() { return d().trust && d().trust.state === "advisory_unlocked"; }
function unavailable() { return missing() || pending(); }
function activeCount() {
  return items().filter((it) => ["进行中", "in_progress", "评审", "review", "验证", "verify", "verification"].includes(it.status)).length;
}
function taskChip(id) {
  const t = item(id);
  return `<button type="button" class="taskchip" data-task="${esc(id)}">${esc(t ? t.title : id)}</button>`;
}
function fmtTime(v) {
  if (!v) return "";
  const s = String(v).replace("T", " ").replace("Z", "").slice(0, 16);
  return s;
}

// ---- nav -------------------------------------------------------------------
function navButton(id, label, count, current) {
  const cnt = count === "" ? "" : `<small class="cnt">${esc(count)}</small>`;
  return `<button type="button" data-section="${esc(id)}" aria-current="${current}"><span>${esc(label)}</span>${cnt}</button>`;
}
function renderNav() {
  const p = state.projects.find((x) => x.id === state.id);
  const dot = !p ? "" : !p.exists ? `<span class="dot bad" style="align-self:flex-start;margin-top:6px"></span>`
    : p.trust === "pending_transaction" ? `<span class="dot warn" style="align-self:flex-start;margin-top:6px"></span>` : "";
  $(".path-row").innerHTML = `<p class="path" id="path">${esc(p ? p.path : "")}</p>${dot}`;

  const nav = $("#nav");
  if (!state.id) {
    nav.innerHTML = navButton("dashboard", "项目总览", "", true);
    return;
  }
  const m = d();
  const its = items();
  const taskBtns = its.map((it) =>
    `<button class="item" type="button" data-task="${esc(it.id)}" aria-current="${state.section === "task" && state.task === it.id}"><span>${esc(it.title)}</span><small>${esc(statusLabel(it.status))}</small></button>`
  ).join("");
  const rec = m.records ? (m.records.artifacts || []).length : 0;
  const wfCount = (m.workflows || []).length;
  const dash = (v) => (unavailable() ? "—" : v);
  nav.innerHTML =
    navButton("dashboard", "项目总览", "", state.section === "dashboard") +
    `<div class="group"><div class="nav-label">项目</div>` +
    navButton("blueprint", "蓝图", "", state.section === "blueprint") +
    navButton("state", "当前状态", "", state.section === "state") +
    `</div><div class="group"><div class="nav-label">任务 ${unavailable() ? "· 读不到" : its.length}</div><div class="sub">` +
    (taskBtns || `<p class="row-sub" style="margin:0 8px">${unavailable() ? "读不到" : "还没有任务"}</p>`) +
    `</div></div><div class="group"><div class="nav-label">其它</div>` +
    navButton("flows", "工作流", dash(wfCount), state.section === "flows") +
    navButton("approvals", "审批", dash(0), state.section === "approvals") +
    navButton("records", "阶段产物", dash(rec), state.section === "records") +
    navButton("runs", "运行", dash(m.runs ? m.runs.entries.length : 0), state.section === "runs") +
    navButton("knowledge", "知识", dash(""), state.section === "knowledge") +
    `</div>`;
}

// ---- workflow svg ----------------------------------------------------------
function flowTrack(wf, current) {
  const steps = wf.steps || [];
  const curIdx = current ? steps.findIndex((s) => s.id === current) : -1;
  const W = 780, H = 120, R = 16, pad = 40, cy = 42;
  const n = steps.length;
  if (!n) return "";
  const gap = n > 1 ? (W - pad * 2) / (n - 1) : 0;
  // adjacency: explicit transitions + implicit linear chain
  const edges = {};
  (wf.transitions || []).forEach((t) => { (edges[t.from] ||= []).push(t.to); });
  steps.forEach((s, i) => { if (i + 1 < n) (edges[s.id] ||= []).push(steps[i + 1].id); });
  const uid = Math.random().toString(36).slice(2, 7);
  const parts = [];
  parts.push(`<line x1="${pad}" y1="${cy}" x2="${W - pad}" y2="${cy}" stroke="var(--line)" stroke-width="1.5" stroke-linecap="round"/>`);
  if (curIdx > 0) parts.push(`<line x1="${pad}" y1="${cy}" x2="${pad + curIdx * gap}" y2="${cy}" stroke="var(--on)" stroke-width="2" stroke-linecap="round"/>`);
  steps.forEach((s, i) => {
    (edges[s.id] || []).forEach((t) => {
      const ti = steps.findIndex((x) => x.id === t);
      if (ti < 0) return;
      const x1 = pad + i * gap, x2 = pad + ti * gap;
      const above = ti < i;
      const yOff = above ? -26 : 26;
      const midX = (x1 + x2) / 2;
      parts.push(`<path d="M${x1},${cy} Q${midX},${cy + yOff * 2} ${x2},${cy}" fill="none" stroke="var(--muted)" stroke-width="1" stroke-dasharray="3 3" opacity="0.55" marker-end="url(#arr-${uid})"><title>${esc(s.id)} → ${esc(t)}</title></path>`);
    });
  });
  steps.forEach((s, i) => {
    const x = pad + i * gap;
    const past = i < curIdx;
    const now = i === curIdx;
    if (now) parts.push(`<circle cx="${x}" cy="${cy}" r="${R + 6}" fill="var(--on)" opacity="0.08"/>`);
    const fill = past ? "var(--on)" : "var(--card)";
    const stroke = past ? "none" : now ? "var(--on)" : "var(--line)";
    const strokeW = past ? 0 : now ? 2.5 : 1.5;
    parts.push(`<circle cx="${x}" cy="${cy}" r="${R}" fill="${fill}" stroke="${stroke}" stroke-width="${strokeW}"><title>${esc(s.type || s.id)}</title></circle>`);
    if (past) parts.push(`<path d="M-4.5,-0.5 L-1.5,3.5 L4.5,-3.5" stroke="var(--card)" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round" transform="translate(${x},${cy})"/>`);
    if (now) parts.push(`<circle cx="${x}" cy="${cy}" r="4" fill="var(--on)"/>`);
    const ly = cy + R + 14;
    parts.push(`<text x="${x}" y="${ly}" text-anchor="middle" font-size="12" fill="${now ? "var(--on)" : past ? "var(--muted)" : "var(--ink)"}" font-weight="${now ? 600 : 450}">${esc(s.id)}</text>`);
    const sub = [s.status ? `需${esc(s.status)}` : "", s.required ? "" : "可选"].filter(Boolean).join(" · ");
    if (sub) parts.push(`<text x="${x}" y="${ly + 13}" text-anchor="middle" font-size="10" fill="var(--muted)">${sub}</text>`);
  });
  return `<svg viewBox="0 0 ${W} ${H}" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${esc(wf.name || wf.id)} 工作流"><defs><marker id="arr-${uid}" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,0 L8,4 L0,8 z" fill="var(--muted)"/></marker></defs>${parts.join("")}</svg>`;
}

// ---- views -----------------------------------------------------------------
function degradedBanner(title, sub, tone) {
  const color = tone === "wait" ? "var(--wait)" : "var(--bad)";
  return `<div class="degraded" style="border-left-color:${color}"><svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="6.2" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M7 3.6v4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/><circle cx="7" cy="10" r="1" fill="currentColor"/></svg><div><div class="d-title">${esc(title)}</div><div class="d-sub">${esc(sub)}</div></div></div>`;
}
function refreshBar() {
  const el = $("#refresh");
  if (!el) return;
  if (state.refreshError) {
    el.textContent = state.refreshError;
    el.classList.add("bad");
    return;
  }
  el.classList.remove("bad");
  el.textContent = state.lastOkAt ? "上次刷新 " + state.lastOkAt.toLocaleTimeString() + " · 每 2 秒轮询当前项目" : "";
}

function renderDashboard() {
  const box = main();
  if (!state.projects.length) {
    box.innerHTML = `<div class="dashboard"><header class="dash-head"><div><h2>项目总览</h2><p class="meta">还没有登记任何项目</p></div></header>${addProjectCard()}</div>`;
    wireAddForm();
    return;
  }
  const rows = state.projects.map((p) => {
    const readable = p.exists && p.tasks != null && p.trust !== "pending_transaction";
    const stateLabel = !p.exists ? "缺失" : p.trust === "pending_transaction" ? "待恢复" : readable && p.knowledge === "stale" ? "有风险" : "可查看";
    const tone = stateLabel === "可查看" ? "st-ok" : stateLabel === "待恢复" || stateLabel === "有风险" ? "st-wait" : "st-bad";
    const active = readable && p.active_tasks != null ? p.active_tasks : "—";
    const foot = !p.exists ? "打开查看降级状态" : p.trust === "pending_transaction" ? (p.reason || "检测到未完成写入") : readable ? `${active} 个任务正在推进 · ${p.runs != null ? p.runs : "—"} 次运行` : (p.reason || "状态不可读取");
    return `<button type="button" class="card project-card" data-repo="${esc(p.id)}" aria-disabled="${p.exists && p.tasks == null}"><div class="card-head"><h3>${esc(p.id)}</h3><span class="status ${tone}">${stateLabel}</span></div><p class="project-path">${esc(p.path)}</p><div class="project-stats"><div><span>任务</span><strong>${readable ? p.tasks : "—"}</strong></div><div><span>风险</span><strong>${readable ? p.risks : "—"}</strong></div><div><span>知识</span><strong>${readable ? esc(knowledgeLabel(p.knowledge)) : "读不到"}</strong></div></div><p class="row-sub" style="margin:12px 0 0">${esc(foot)}</p></button>`;
  }).join("");
  const readableRows = state.projects.filter((p) => p.exists && p.tasks != null);
  const totalTasks = readableRows.reduce((n, p) => n + (p.tasks || 0), 0);
  const active = readableRows.reduce((n, p) => n + (p.active_tasks || 0), 0);
  const risks = readableRows.reduce((n, p) => n + (p.risks || 0), 0);
  box.innerHTML = `<div class="dashboard"><header class="dash-head"><div><h2>项目总览</h2><p class="meta">已登记项目的只读状态快照 · 点击项目卡进入详情</p></div><span class="rid">${state.projects.length} 个项目</span></header><div class="dash-summary"><div class="stat-card"><span>项目</span><b>${state.projects.length}</b></div><div class="stat-card"><span>任务</span><b>${totalTasks}</b></div><div class="stat-card"><span>进行中</span><b>${active}</b></div><div class="stat-card"><span>风险 / 阻塞</span><b>${risks}</b></div></div><h3 class="section-label">项目状态</h3><div class="project-grid">${rows}</div><h3 class="section-label">添加项目</h3>${addProjectCard()}</div>`;
  wireAddForm();
}

function addProjectCard() {
  return `<section class="card"><h3>添加项目</h3><p class="row-sub">登记一个本机已有 <code>.devsys</code> 目录的项目路径，立即加入上方清单。</p><form id="add-form" class="add-form"><label class="row-sub" style="width:100%;margin:0">本地路径<input id="add-path" name="path" type="text" required autocomplete="off" spellcheck="false" placeholder="C:/Source/my-project"></label><label class="row-sub" style="flex:1;min-width:160px;margin:0">项目 ID（可选）<input id="add-id" name="id" type="text" autocomplete="off" spellcheck="false" placeholder="缺省时取目录名"></label><button type="submit" ${state.addBusy ? "disabled" : ""}>${state.addBusy ? "添加中…" : "添加项目"}</button></form><p id="add-msg" class="form-msg ${state.addError ? "bad" : ""}" role="alert">${esc(state.addError)}</p></section>`;
}
function wireAddForm() {
  const form = $("#add-form");
  if (!form) return;
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const path = $("#add-path").value.trim();
    const idRaw = $("#add-id").value.trim();
    state.addError = "";
    if (!path) { state.addError = "请填写本地路径"; render(); return; }
    state.addBusy = true;
    render();
    try {
      const payload = { path };
      if (idRaw) payload.id = idRaw;
      const entry = await apiPost("/api/projects", payload);
      await loadProjects();
      selectProject(entry.id || idRaw, true);
    } catch (err) {
      state.addError = err.message || "添加失败";
      state.addBusy = false;
      render();
    }
  });
}

function renderBlueprint() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<div class="empty"><b>蓝图不可用</b>恢复路径后自动重新加载。</div>`;
    return;
  }
  const m = d();
  const b = m.project || {};
  const scope = b.scope || {};
  const ms = b.milestones || [];
  const li = (g, dim) => `<li${dim ? ' class="dim"' : ""}>${esc(g)}</li>`;
  const milestones = ms.length ? `<section class="card"><div class="card-head"><h3>里程碑</h3><span class="card-count">${ms.filter((x) => x.status === "open").length} 开启 · ${ms.filter((x) => x.status !== "open").length} 完成</span></div><div class="ms-track"><i style="width:${Math.round(ms.filter((x) => x.status !== "open").length / ms.length * 100)}%"></i></div>${ms.map((x) => `<div class="ms-row"><span class="dot ${x.status === "open" ? "open" : "done"}"></span><span class="ms-name">${esc(x.name)}</span><code class="ms-id">${esc(x.id)}</code></div>`).join("")}</section>` : "";
  const goals = (b.goals || []).concat(b.tech_stack && b.tech_stack.length ? ["技术栈：" + b.tech_stack.join("、")] : []);
  const grid = (goals.length || (scope.in || []).length || (scope.out || []).length || (b.constraints || []).length)
    ? `<h3 class="section-label">方向</h3><div class="bp-grid"><section class="card"><h3>目标</h3><ul>${goals.map((g) => li(g)).join("") || '<li class="row-sub">无</li>'}</ul></section><section class="card"><h3>范围内</h3><ul>${(scope.in || []).map((g) => li(g)).join("") || '<li class="row-sub">无</li>'}</ul></section><section class="card"><h3>范围外</h3><ul>${(scope.out || []).map((g) => li(g, true)).join("") || '<li class="row-sub">无</li>'}</ul></section><section class="card"><h3>约束</h3><ul>${(b.constraints || []).map((g) => li(g, true)).join("") || '<li class="row-sub">无</li>'}</ul></section></div>` : "";
  const bp = b.blueprint;
  const artifactMeta = bp ? `<section class="card blueprint-artifact"><div class="card-head"><h3>蓝图产物</h3><span class="card-count">${esc(bp.status || "未知")}${bp.version ? ` · v${bp.version}` : ""}</span></div><p class="row-sub"><code>${esc(bp.path || bp.name || bp.id)}</code></p>${bp.contentError ? `<div class="empty"><b>正文不可用</b>${esc(bp.contentError)}</div>` : bp.content ? `<pre class="blueprint-content">${esc(bp.content)}</pre>` : `<div class="empty"><b>没有蓝图正文</b>该 artifact 未关联源文件。</div>`}</section>` : "";
  box.innerHTML = `<header class="bp-head">${b.current_phase ? `<div class="bp-eyebrow"><span class="pill-solid">${esc(b.current_phase)}</span></div>` : ""}<h2 class="bp-title">${esc(b.name || state.id)}</h2>${b.status && !pending() ? `<p class="meta">项目状态 · ${statusDot(b.status)}</p>` : ""}${b.description ? `<p class="bp-lead">${esc(b.description)}</p>` : ""}${bp ? `<p class="meta">蓝图产物 · ${esc(bp.name || bp.id)}${bp.version ? ` v${bp.version}` : ""}</p>` : ""}</header>${artifactMeta}${milestones}${grid}`;
}

function renderState() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<div class="empty"><b>状态不可用</b>恢复路径后自动重新加载。</div>`;
    return;
  }
  const m = d();
  const p = m.project || {};
  const r = (m.progress && m.progress.readiness) || {};
  const risks = p.risks || [];
  const blockers = p.blockers || [];
  const issues = (risks.length || blockers.length) ? `<div class="state-alerts">${risks.length ? `<section class="alert-card warn"><h3>风险</h3>${risks.map((v) => `<p>${esc(v)}</p>`).join("")}</section>` : ""}${blockers.length ? `<section class="alert-card bad"><h3>阻塞</h3>${blockers.map((v) => `<p>${esc(v)}</p>`).join("")}</section>` : ""}</div>` : "";
  const riskLis = (r.risks || []).map((x) => `<li>${esc(x.detail || x.kind)}${x.workitem_id ? ` · ${taskChip(x.workitem_id)}` : ""}</li>`).join("");
  const fixLis = (r.fixes || []).map((x) => `<li>${esc(x.reason)}${x.command ? ` · <code>${esc(x.command)}</code>` : ""}</li>`).join("");
  const verdictLabel = r.verdict === "PASS" ? "通过" : r.verdict === "FAIL" ? "失败" : "有风险";
const ACTION_LABEL = { recover_claim: "恢复领取", review: "评审", start: "开始", start_backlog: "从待办开始", promote_to_ready: "提升为就绪", milestone_review: "里程碑评审", declare_blueprint: "声明蓝图", report_done: "汇报完成" };
  const nextText = (n) => n ? [ACTION_LABEL[n.action] || n.action, n.workitem_id, n.reason].filter(Boolean).join(" · ") : "";
  const readiness = (r.verdict || (r.risks || []).length || (r.fixes || []).length) ? `<section class="card"><div class="card-head"><h3>就绪判断</h3>${statusDot(verdictLabel)}</div>${(r.reasons || []).length ? `<p class="row-sub" style="margin:0 0 6px">${(r.reasons || []).map(esc).join(" · ")}</p>` : ""}${riskLis ? `<ul>${riskLis}</ul>` : `<p class="row-sub">没有额外风险</p>`}${fixLis ? `<ul>${fixLis}</ul>` : ""}${nextText(r.next) ? `<p style="margin:10px 0 0;font-size:13px">${esc(nextText(r.next))}</p>` : ""}</section>` : "";
  const bl = m.baseline;
  const baseline = bl && (bl.available || bl.branch || bl.commit) ? `<div class="facts">${bl.available ? `<span>Git 基线 <strong><code>${esc(bl.branch || "?")} · ${esc((bl.commit || "").slice(0, 7))}</code></strong></span><span>工作区 <strong>${bl.dirty ? `有 ${bl.changed_files || 0} 个变更文件` : "干净"}</strong></span>` : `<span>Git 基线 <strong>${esc(bl.reason || "不可用")}</strong></span>`}</div>` : "";
  const degraded = m.project && m.project.degraded ? degradedBanner("状态降级", m.project.reason || "部分状态文件读取失败，以上仅供参考", "wait") : "";
  box.innerHTML = `<h2>当前状态</h2>${degraded}<div class="state-summary">${esc(p.summary || "暂无摘要")}</div>${issues}<div class="state-next"><section class="card"><h3>下一步重点</h3><ul>${(p.next_focus || []).map((v) => `<li>${esc(v)}</li>`).join("") || '<li class="row-sub">无</li>'}</ul></section>${readiness || `<section class="card"><h3>下一步建议</h3><p style="margin:0;font-size:13px">${esc(nextText(r.next) || "暂无建议")}</p></section>`}</div>${baseline}`;
}

function renderTask() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<h2>任务读不到</h2><div class="empty"><b>任务不可用</b>目录缺失，任务列表读不到。</div>`;
    return;
  }
  const it = item(state.task);
  if (!it) {
    const its = items();
    box.innerHTML = its.length ? `<h2>任务</h2><div class="empty"><b>未选择任务</b>从左侧任务列表选择一个任务查看详情。</div>` : `<h2>任务</h2><div class="empty"><b>还没有任务</b>新工作项会出现在这里。</div>`;
    return;
  }
  const wfs = d().workflows || [];
  const wf = wfs.find((w) => w.id === it.workflow);
  const deps = it.dependencies || [];
  const arts = ((d().records && d().records.artifacts) || []).filter((a) => a.workitem_id === it.id || (a.related_workitems || []).includes(it.id));
  const runs = ((d().runs && d().runs.entries) || []).filter((r) => r.workitem_id === it.id);
  const strip = `<div class="strip"><div class="cell"><span class="clabel">状态</span><span class="cvalue">${statusDot(it.status)}</span></div><div class="cell"><span class="clabel">优先级</span><span class="cvalue sm" style="${it.priority >= 8 ? "font-weight:600" : ""}">${it.priority >= 8 ? "高" : it.priority >= 5 ? "中" : "低"}</span></div>${it.assigned_agent ? `<div class="cell"><span class="clabel">领取人</span><span class="cvalue sm">${esc(it.assigned_agent)}</span></div>` : ""}<div class="cell"><span class="clabel">依赖</span><span class="cvalue sm">${deps.length ? deps.map(taskChip).join(" ") : "无"}</span></div></div>`;
  const details = [["父任务", it.parent_id], ["Harness", it.assigned_harness], ["调度", it.scheduling_state], ["租约持有人", it.lease_owner], ["租约到期", fmtTime(it.lease_until)], ["下次重试", fmtTime(it.next_attempt_at)], ["活动运行", it.active_run_id]].filter(([, v]) => v);
  const flowHtml = wf
    ? `<div class="flowzone"><h3>工作流 · ${esc(wf.name || wf.id)}${it.workflow_paused ? '<span class="status" style="color:var(--muted)"><span class="dot idle" style="box-shadow:none"></span>已暂停</span>' : ""}</h3>${flowTrack(wf, it.workflow_step)}<p class="flownote">推进步骤不会改状态；改状态不会移动步骤。</p></div>`
    : `<div class="flowzone"><h3>工作流</h3><p class="row-sub">没绑定。状态仍然可以改。</p></div>`;
  const related = `<div class="related-grid"><section class="card"><h3>运行</h3>${runs.length ? runs.map((r) => `<div class="row"><span class="rid">${esc(r.id)}</span><span>${esc(r.phase || r.status)}<span class="row-sub">${esc([r.agent_id, r.summary, r.errors && r.errors[0]].filter(Boolean).join(" · "))}</span></span><span>${statusDot(r.status)}</span></div>`).join("") : '<p class="row-sub">当前任务还没有运行记录。</p>'}</section><section class="card"><h3>阶段产物</h3>${arts.length ? arts.map((a) => `<div class="row"><span class="rid">${esc(a.id)}</span><span>${esc(a.title || a.type)}<span class="row-sub">${esc([a.type, a.stage, a.status, a.version ? "v" + a.version : ""].filter(Boolean).join(" · "))}</span></span><span></span></div>`).join("") : '<p class="row-sub">当前任务还没有阶段产物。</p>'}</section><section class="card"><h3>Timeline</h3><p class="row-sub">完整 Timeline 详情尚未进入当前 view 模型；请使用任务关联的 Run/Event 查询。</p></section></div>`;
  box.innerHTML = `<div class="detail"><div class="detail-head"><div class="detail-title"><h2>${esc(it.title)}</h2><span class="rid">${esc(it.id)}</span>${statusDot(it.status)}</div></div>${strip}${details.length ? `<div class="facts">${details.map(([k, v]) => `<span>${esc(k)} <strong>${esc(v)}</strong></span>`).join("")}</div>` : ""}${flowHtml}${related}</div>`;
}

function renderFlows() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<h2>工作流</h2><div class="empty"><b>读不到工作流</b>目录缺失，不能判断流程定义。</div>`;
    return;
  }
  const wfs = d().workflows || [];
  if (!wfs.length) { box.innerHTML = `<h2>这个仓的工作流</h2><div class="empty"><b>还没有工作流</b>在 .devsys/workflows/ 里定义流程后会出现在这里。</div>`; return; }
  box.innerHTML = `<h2>这个仓的工作流</h2><p class="meta">一份流程可被多个任务共用。虚线弧线标注分叉。</p><div class="stack">${wfs.map((wf) => { const users = items().filter((it) => it.workflow === wf.id); return `<section class="card"><div class="card-head"><h3>${esc(wf.name || wf.id)}</h3><span class="card-count">${users.length} 个任务使用 · ${(wf.steps || []).length} 步</span></div>${flowTrack(wf, null)}<div style="margin-top:10px;display:flex;gap:8px;flex-wrap:wrap">${users.length ? users.map((it) => taskChip(it.id)).join("") : '<span class="row-sub">还没有任务使用</span>'}</div></section>`; }).join("")}</div>`;
}

function renderApprovals() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<h2>审批</h2><div class="empty"><b>审批读不到</b>目录缺失，不能判断审批状态。</div>`;
    return;
  }
  // pending approvals surface via readiness risks (kind pending_approval)
  const r = (d().progress && d().progress.readiness) || {};
  const pendingRisks = (r.risks || []).filter((x) => (x.kind || "").includes("approval"));
  if (!pendingRisks.length) { box.innerHTML = `<h2>审批</h2><p class="meta">每条审批卡着一个任务的状态流转。</p><div class="empty"><b>没有待处理的审批</b>任务状态需要批准时会出现在这里。</div>`; return; }
  box.innerHTML = `<h2>审批</h2><p class="meta">每条审批卡着一个任务的状态流转。</p><div class="list"><div class="list-head"><span>任务</span><span>内容</span><span>状态</span></div>${pendingRisks.map((x) => `<div class="row"><span>${x.workitem_id ? taskChip(x.workitem_id) : '<span class="rid">—</span>'}</span><span>${esc(x.detail || x.kind)}</span><span>${statusDot("申请中")}</span></div>`).join("")}</div>`;
}

function renderRecords() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<h2>记录</h2><div class="empty"><b>记录读不到</b>目录缺失，不能判断记录状态。</div>`;
    return;
  }
  const rec = d().records || { artifacts: [] };
  const rows = rec.artifacts || [];
  if (!rows.length) { box.innerHTML = `<h2>阶段产物</h2><p class="meta">Decision、Finding 和验证结果按 Artifact 类型归档，来源绑定任务、Run 和阶段。</p><div class="empty"><b>还没有阶段产物</b>任务推进后，规格、决策、研究和验证证据会出现在这里。</div>`; return; }
  box.innerHTML = `<h2>阶段产物</h2><p class="meta">Decision、Finding 和验证结果按 Artifact 类型归档，来源绑定任务、Run 和阶段。</p><div class="list"><div class="subhead">Artifact</div>${rows.map((x) => `<div class="row"><span class="rid">${esc(x.id)}</span><span>${esc(x.title || x.type || "")}<span class="row-sub">${esc([x.type, x.stage, x.status, x.version ? "v" + x.version : "", x.workitem_id || (x.related_workitems || []).join(", ")].filter(Boolean).join(" · "))}</span></span><span></span></div>`).join("")}</div>`;
}

function renderRuns() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<h2>运行</h2><div class="empty"><b>运行读不到</b>目录缺失，不能判断运行状态。</div>`;
    return;
  }
  const entries = (d().runs && d().runs.entries) || [];
  if (!entries.length) { box.innerHTML = `<h2>运行</h2><p class="meta">每次本地运行的留痕。</p><div class="empty"><b>还没有运行</b>本地命令执行后会留下记录。</div>`; return; }
  box.innerHTML = `<h2>运行</h2><p class="meta">每次本地运行的留痕。</p><div class="list"><div class="list-head"><span>ID</span><span>内容</span><span>状态</span></div>${entries.map((r) => { const t = item(r.workitem_id); return `<div class="row"><span class="rid">${esc(r.id)}</span><span>${t ? taskChip(t.id) : r.workitem_id ? `<span class="rid">${esc(r.workitem_id)}</span>` : ""}<span class="row-sub">${esc([r.agent_id, r.harness, `第 ${r.attempt} 次`, r.summary, (r.errors || [])[0]].filter(Boolean).join(" · "))}</span></span><span>${statusDot(r.status)}</span></div>`; }).join("")}</div>`;
}

function renderKnowledge() {
  const box = main();
  if (missing()) {
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`) + `<h2>知识</h2><div class="empty"><b>知识读不到</b>目录缺失，新鲜度无法判断。</div>`;
    return;
  }
  const k = d().knowledge || {};
  const pages = k.pages || [];
  const label = k.status === "fresh" ? "知识新鲜" : k.status === "stale" ? "知识已过期" : k.status === "missing" ? "没有知识页面层" : "知识新鲜度判断不了";
  box.innerHTML = `<h2>知识</h2><p class="meta">repowiki 页面相对代码的新鲜度。</p><section class="card"><div class="k-head"><span class="dot ${K_DOT[k.status] || "bad"}"></span><b>${esc(label)}</b>${k.index_ready ? `<span class="k-time">索引可用 · 变更 ${k.changed_files || 0} 个文件</span>` : ""}</div>${k.reason ? `<p class="row-sub" style="margin:10px 0 0">${esc(k.reason)}</p>` : ""}${pages.length ? `<div class="k-pages">${pages.map((p) => `<div class="k-page"><span>受影响页面</span><span class="rid" style="margin-left:auto">${esc(p.path)}</span></div>`).join("")}</div>` : `<p class="row-sub" style="margin:10px 0 0">没有受影响的页面。</p>`}</section>`;
}

function renderTrustBlocked() {
  const m = d();
  const note = (m.trust && m.trust.note) || "检测到未完成写入";
  main().innerHTML = degradedBanner("待恢复事务 · 不显示业务事实", `${note} · 请先运行 workloom recover`, "wait") + `<h2>${esc((m.project && m.project.name) || state.id)}</h2><div class="empty"><b>状态暂不可用</b>恢复事务后，任务、审批、记录和知识会重新读取。</div>`;
}

function render() {
  renderNav();
  const box = main();
  if (!state.id) { renderDashboard(); return; }
  if (state.section === "dashboard") { renderDashboard(); return; }
  const prefix = `<p id="refresh"></p>`;
  if (missing()) {
    // keep the last-known model only for display of residual facts; sections degrade
    const p = state.projects.find((x) => x.id === state.id);
    box.innerHTML = prefix;
    refreshBar();
    const banner = degradedBanner("目录缺失", `${p ? p.path : state.id} 不存在 · 这里只保留注册表残留信息`);
    if (state.section === "blueprint") renderBlueprint();
    else if (state.section === "state") renderState();
    else if (state.section === "task") renderTask();
    else if (state.section === "flows") renderFlows();
    else if (state.section === "approvals") renderApprovals();
    else if (state.section === "records") renderRecords();
    else if (state.section === "runs") renderRuns();
    else if (state.section === "knowledge") renderKnowledge();
    box.insertAdjacentHTML("afterbegin", banner);
    return;
  }
  if (pending()) { box.innerHTML = prefix; refreshBar(); renderTrustBlocked(); return; }
  box.innerHTML = prefix;
  refreshBar();
  if (state.section === "blueprint") renderBlueprint();
  else if (state.section === "state") renderState();
  else if (state.section === "task") renderTask();
  else if (state.section === "flows") renderFlows();
  else if (state.section === "approvals") renderApprovals();
  else if (state.section === "records") renderRecords();
  else if (state.section === "runs") renderRuns();
  else if (state.section === "knowledge") renderKnowledge();
}

// ---- data loading ----------------------------------------------------------
async function loadProjects() {
  const r = await apiGet("/api/projects");
  if (r.body) state.projects = r.body;
}
let modelLoading = false;
async function loadModel(preserveScroll = false) {
  if (!state.id || modelLoading) return;
  modelLoading = true;
  const url = `/api/projects/${encID(state.id)}/view`;
  const scrollTarget = preserveScroll ? document.querySelector(".blueprint-content, main") : null;
  const scrollTop = scrollTarget ? scrollTarget.scrollTop : 0;
  try {
    const r = await apiGet(url);
    if (r.notModified) return;
    if (r.body) {
      state.model = r.body;
      state.lastGood[state.id] = r.body;
      state.refreshError = "";
      state.lastOkAt = new Date();
    }
  } catch (e) {
    state.refreshError = "无法刷新 · 显示的是上次成功的内容";
    if (state.lastGood[state.id]) state.model = state.lastGood[state.id];
    else state.model = null;
  } finally {
    modelLoading = false;
  }
  render();
  if (preserveScroll) (document.querySelector(".blueprint-content, main") || {}).scrollTop = scrollTop;
}

function selectProject(id, firstSection) {
  state.id = id;
  state.section = firstSection ? "blueprint" : (state.section === "dashboard" ? "blueprint" : state.section);
  state.model = state.lastGood[id] || null;
  const its = items();
  if (firstSection || !its.some((it) => it.id === state.task)) state.task = its.length ? its[0].id : "";
  state.refreshError = "";
  render();
  if (id) loadModel();
}

let timer = null;
function startPolling() {
  if (timer) clearInterval(timer);
  timer = setInterval(() => {
    if (!document.hidden && state.id && !unavailable()) loadModel(true);
  }, 2000);
}

// ---- events ----------------------------------------------------------------
function bind() {
  $("#nav").addEventListener("click", (e) => {
    const t = e.target.closest("[data-task]");
    const s = e.target.closest("[data-section]");
    if (t) { state.section = "task"; state.task = t.dataset.task; }
    else if (s) state.section = s.dataset.section;
    else return;
    render();
  });
  main().addEventListener("click", (e) => {
    const card = e.target.closest("[data-repo]");
    if (card) { selectProject(card.dataset.repo, true); $("#project").value = state.id; return; }
    const chip = e.target.closest(".taskchip[data-task]");
    if (!chip) return;
    state.section = "task";
    state.task = chip.dataset.task;
    render();
  });
  $("#nav-toggle").addEventListener("click", () => {
    const nav = document.querySelector("nav");
    const collapsed = nav.classList.toggle("collapsed");
    $("#nav-toggle").setAttribute("aria-expanded", String(!collapsed));
  });
  $("#project").addEventListener("change", (e) => {
    if (e.target.value === "__add__") {
      renderProjectSelect();
      state.section = "dashboard";
      render();
      const input = $("#add-path");
      if (input) input.focus();
      return;
    }
    selectProject(e.target.value, true);
  });
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden && state.id && !unavailable()) loadModel(true);
  });
}

function renderProjectSelect() {
  const sel = $("#project");
  if (!state.projects.length) {
    sel.innerHTML = `<option value="">（无项目）</option>`;
    sel.disabled = true;
    return;
  }
  sel.disabled = false;
  sel.innerHTML = state.projects.map((p) => {
    const label = p.id + (p.exists ? "" : " · 缺失") + (p.trust === "pending_transaction" ? " · 待恢复" : "");
    return `<option value="${esc(p.id)}">${esc(label)}</option>`;
  }).join("") + `<option value="__add__">＋ 添加项目…</option>`;
  sel.value = state.id || state.projects[0].id;
}

// ---- boot ------------------------------------------------------------------
// ponytail: hash deep links (#project/section/task) exist only for smoke tests
// and manual debugging; production navigation is click-driven.
function applyHash() {
  const h = location.hash.replace(/^#\/?/, "");
  if (!h) return false;
  const [pid, sec, tid] = h.split("/").map(decodeURIComponent);
  if (!state.projects.some((p) => p.id === pid)) return false;
  selectProject(pid, false);
  if (sec) state.section = sec;
  if (tid) state.task = tid;
  render();
  return true;
}
(async () => {
  bind();
  try {
    await loadProjects();
    renderProjectSelect();
    if (!applyHash()) {
      if (state.projects.length) selectProject(state.projects[0].id, true);
      else { state.id = ""; render(); }
      renderProjectSelect();
    }
    state.started = true;
    startPolling();
  } catch (e) {
    main().innerHTML = `<div class="degraded"><div><div class="d-title">Hub 启动失败</div><div class="d-sub">${esc(e.message)}</div></div></div>`;
  }
})();
})();
