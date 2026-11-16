(() => {
  "use strict";

  const I18N = {
    en: {
      "nav.main": "Main navigation",
      "nav.overview": "Overview",
      "nav.memory": "Memory",
      "nav.schedules": "Schedules",
      "nav.settings": "Settings",
      "nav.logout": "Log out",
      "app.eyebrow": "LOCAL CONTROL PLANE",
      "page.overview": "Overview",
      "page.memory": "Memory",
      "page.schedules": "Schedules",
      "page.settings": "Settings",
      "readonly.badge": "Read-only",
      "common.language": "Language",
      "common.refresh": "Refresh",
      "common.close": "Close",
      "common.cancel": "Cancel",
      "common.save": "Save",
      "common.query": "Query",
      "common.edit": "Edit",
      "common.delete": "Delete",
      "common.approve": "Approve",
      "common.archive": "Archive",
      "common.pin": "Pin",
      "common.unpin": "Unpin",
      "overview.eyebrow": "RUNTIMES",
      "overview.botStatus": "Bot status",
      "overview.recentErrors": "Recent errors",
      "overview.noErrors": "No errors.",
      "overview.runtimes": "{count} runtimes",
      "overview.summary.bots": "Running bots",
      "overview.summary.botsHint": "Up {uptime}",
      "overview.summary.activeMemories": "Active memories",
      "overview.summary.pendingMemories": "{count} pending review",
      "overview.summary.activeSchedules": "Active schedules",
      "overview.summary.running": "{count} running",
      "overview.summary.noRunning": "No runs in progress",
      "overview.summary.mode": "Console mode",
      "overview.summary.readOnly": "Read-only",
      "overview.summary.writable": "Writable",
      "overview.summary.noConfigPath": "Config path unavailable",
      "overview.defaultModel": "default model",
      "overview.metric.activeMemory": "active memory",
      "overview.metric.pendingMemory": "pending memory",
      "overview.metric.activeSchedule": "active schedule",
      "overview.path.workspace": "workspace",
      "overview.path.data": "data",
      "overview.path.nextRun": "next run",
      "memory.eyebrow": "MEMORY STORE",
      "memory.title": "Memory management",
      "memory.searchPlaceholder": "Search memory content, projects, or tags…",
      "memory.allStatuses": "All statuses",
      "memory.status.candidate": "Pending review",
      "memory.status.active": "Active",
      "memory.status.archived": "Archived",
      "memory.allScopes": "All scopes",
      "memory.allTypes": "All types",
      "memory.query": "Query",
      "memory.results": "{count} results",
      "memory.empty": "No matching memories.",
      "memory.count.pending": "Pending",
      "memory.count.active": "Active",
      "memory.count.archived": "Archived",
      "memory.used": "used {count}",
      "memory.updated": "updated {time}",
      "memory.pinned": "pinned",
      "memory.toast.updated": "Memory updated",
      "memory.confirmDelete": "Delete this memory? It will no longer be injected.",
      "memory.toast.deleted": "Memory deleted",
      "memory.toast.saved": "Memory content saved",
      "memory.dialog.title": "Edit memory",
      "schedule.eyebrow": "SCHEDULER",
      "schedule.title": "Scheduled tasks",
      "schedule.showCompleted": "Show completed",
      "schedule.new": "New task",
      "schedule.count": "{count} tasks",
      "schedule.empty": "No scheduled tasks yet.",
      "schedule.running": "running",
      "schedule.status.active": "active",
      "schedule.status.paused": "paused",
      "schedule.status.completed": "completed",
      "schedule.status.failed": "failed",
      "schedule.fact.next": "next",
      "schedule.fact.last": "last",
      "schedule.fact.runs": "runs",
      "schedule.fact.target": "target",
      "schedule.fact.tz": "tz",
      "schedule.fact.timeout": "timeout",
      "schedule.action.pause": "Pause",
      "schedule.action.run": "Run now",
      "schedule.action.resume": "Resume",
      "schedule.action.cancel": "Cancel run",
      "schedule.action.edit": "Edit",
      "schedule.action.delete": "Delete",
      "schedule.toast.updated": "Task status updated",
      "schedule.confirmDelete": "Delete scheduled task \"{name}\"?",
      "schedule.toast.deleted": "Scheduled task deleted",
      "schedule.toast.saved": "Scheduled task updated",
      "schedule.toast.created": "Scheduled task created",
      "schedule.dialog.create": "New scheduled task",
      "schedule.dialog.edit": "Edit scheduled task",
      "schedule.form.mode": "Mode",
      "schedule.mode.every": "Fixed interval",
      "schedule.mode.in": "Delay once",
      "schedule.mode.at": "Specific time",
      "schedule.form.cron": "Cron expression",
      "schedule.form.interval": "Interval",
      "schedule.form.delay": "Delay",
      "schedule.form.at": "Execution time",
      "schedule.form.target": "Delivery target",
      "schedule.form.timezone": "Timezone",
      "schedule.form.timeout": "Timeout",
      "schedule.form.description": "Description",
      "schedule.form.descriptionPlaceholder": "Optional, e.g. daily standup summary",
      "schedule.form.prompt": "Prompt",
      "schedule.form.promptPlaceholder": "Prompt the agent should receive when the task fires",
      "schedule.previewButton": "Preview next runs",
      "schedule.preview.next": "Next: {runs}",
      "schedule.preview.none": "No execution times to preview.",
      "schedule.noTargets": "No delivery target yet: send a message in a chat first",
      "schedule.readonly": "Console is in read-only mode",
      "settings.eyebrow": "READ-ONLY CONFIG",
      "settings.title": "Effective config",
      "settings.secrets": "Secrets and credentials never leave the process",
      "settings.runtime": "Runtime",
      "settings.memory": "Memory",
      "settings.schedule": "Schedule",
      "settings.default": "default",
      "settings.yes": "Yes",
      "settings.no": "No",
      "settings.field.id": "ID",
      "settings.field.name": "name",
      "settings.field.platform": "platform",
      "settings.field.engine": "engine",
      "settings.field.model": "model",
      "settings.field.workspace": "workspace",
      "settings.field.workspaceMode": "workspace mode",
      "settings.field.dataDir": "data dir",
      "settings.field.adminUsers": "admin users",
      "settings.field.allowedChats": "allowed chats",
      "settings.field.enabled": "enabled",
      "settings.field.defaultScope": "default scope",
      "settings.field.maxItems": "max items",
      "settings.field.maxChars": "max chars",
      "settings.field.autoLearnFailures": "auto learn failures",
      "settings.field.timezone": "timezone",
      "settings.field.historyLimit": "history limit",
      "settings.field.maxConcurrent": "max concurrent",
      "settings.field.jitter": "jitter",
      "settings.field.timeout": "timeout",
      "settings.configPath": "Config file: {path}. The console does not edit it; restart the service after changes.",
      "settings.configNotice": "The console does not edit the config file; restart the service after changes.",
      "error.loginExpired": "Session expired",
      "error.requestFailed": "Request failed",
      "toast.refreshed": "Refreshed",
    },
    zh: {
      "nav.main": "主导航",
      "nav.overview": "总览",
      "nav.memory": "记忆",
      "nav.schedules": "定时任务",
      "nav.settings": "设置",
      "nav.logout": "退出",
      "app.eyebrow": "本地控制面",
      "page.overview": "总览",
      "page.memory": "记忆",
      "page.schedules": "定时任务",
      "page.settings": "设置",
      "readonly.badge": "只读模式",
      "common.language": "语言",
      "common.refresh": "刷新",
      "common.close": "关闭",
      "common.cancel": "取消",
      "common.save": "保存",
      "common.query": "查询",
      "common.edit": "编辑",
      "common.delete": "删除",
      "common.approve": "批准",
      "common.archive": "归档",
      "common.pin": "固定",
      "common.unpin": "取消固定",
      "overview.eyebrow": "运行时",
      "overview.botStatus": "Bot 状态",
      "overview.recentErrors": "最近异常",
      "overview.noErrors": "暂无异常。",
      "overview.runtimes": "{count} 个运行时",
      "overview.summary.bots": "运行中的 Bot",
      "overview.summary.botsHint": "已运行 {uptime}",
      "overview.summary.activeMemories": "已启用记忆",
      "overview.summary.pendingMemories": "{count} 条待审核",
      "overview.summary.activeSchedules": "活跃定时任务",
      "overview.summary.running": "{count} 个正在执行",
      "overview.summary.noRunning": "暂无执行中的任务",
      "overview.summary.mode": "控制台模式",
      "overview.summary.readOnly": "只读",
      "overview.summary.writable": "可写",
      "overview.summary.noConfigPath": "未返回配置路径",
      "overview.defaultModel": "默认模型",
      "overview.metric.activeMemory": "已启用记忆",
      "overview.metric.pendingMemory": "待审核记忆",
      "overview.metric.activeSchedule": "活跃任务",
      "overview.path.workspace": "工作区",
      "overview.path.data": "数据",
      "overview.path.nextRun": "下次执行",
      "memory.eyebrow": "记忆存储",
      "memory.title": "记忆管理",
      "memory.searchPlaceholder": "搜索记忆内容、项目或标签…",
      "memory.allStatuses": "全部状态",
      "memory.status.candidate": "待审核",
      "memory.status.active": "已启用",
      "memory.status.archived": "已归档",
      "memory.allScopes": "全部 scope",
      "memory.allTypes": "全部类型",
      "memory.query": "查询",
      "memory.results": "{count} 条结果",
      "memory.empty": "没有匹配的记忆。",
      "memory.count.pending": "待审核",
      "memory.count.active": "已启用",
      "memory.count.archived": "已归档",
      "memory.used": "使用 {count} 次",
      "memory.updated": "更新于 {time}",
      "memory.pinned": "已固定",
      "memory.toast.updated": "记忆已更新",
      "memory.confirmDelete": "确定删除这条记忆？删除后不会参与注入。",
      "memory.toast.deleted": "记忆已删除",
      "memory.toast.saved": "记忆内容已保存",
      "memory.dialog.title": "编辑记忆",
      "schedule.eyebrow": "调度器",
      "schedule.title": "定时任务",
      "schedule.showCompleted": "显示已完成",
      "schedule.new": "新建任务",
      "schedule.count": "{count} 个任务",
      "schedule.empty": "还没有定时任务。",
      "schedule.running": "执行中",
      "schedule.status.active": "活跃",
      "schedule.status.paused": "已暂停",
      "schedule.status.completed": "已完成",
      "schedule.status.failed": "失败",
      "schedule.fact.next": "下次",
      "schedule.fact.last": "上次",
      "schedule.fact.runs": "次数",
      "schedule.fact.target": "目标",
      "schedule.fact.tz": "时区",
      "schedule.fact.timeout": "超时",
      "schedule.action.pause": "暂停",
      "schedule.action.run": "立即运行",
      "schedule.action.resume": "恢复",
      "schedule.action.cancel": "取消执行",
      "schedule.action.edit": "编辑",
      "schedule.action.delete": "删除",
      "schedule.toast.updated": "任务状态已更新",
      "schedule.confirmDelete": "确定删除定时任务「{name}」？",
      "schedule.toast.deleted": "定时任务已删除",
      "schedule.toast.saved": "定时任务已更新",
      "schedule.toast.created": "定时任务已创建",
      "schedule.dialog.create": "新建定时任务",
      "schedule.dialog.edit": "编辑定时任务",
      "schedule.form.mode": "模式",
      "schedule.mode.every": "固定间隔",
      "schedule.mode.in": "延迟一次",
      "schedule.mode.at": "指定时间",
      "schedule.form.cron": "Cron 表达式",
      "schedule.form.interval": "间隔",
      "schedule.form.delay": "延迟",
      "schedule.form.at": "执行时间",
      "schedule.form.target": "投递目标",
      "schedule.form.timezone": "时区",
      "schedule.form.timeout": "超时",
      "schedule.form.description": "描述",
      "schedule.form.descriptionPlaceholder": "可选，例如：每日站会摘要",
      "schedule.form.prompt": "Prompt",
      "schedule.form.promptPlaceholder": "到点后交给 Agent 的完整提示词",
      "schedule.previewButton": "预览下次执行",
      "schedule.preview.next": "接下来：{runs}",
      "schedule.preview.none": "没有可预览的执行时间。",
      "schedule.noTargets": "还没有投递目标：先在聊天里发一条消息",
      "schedule.readonly": "控制台处于只读模式",
      "settings.eyebrow": "只读配置",
      "settings.title": "生效配置",
      "settings.secrets": "密钥与凭证不会离开进程",
      "settings.runtime": "运行时",
      "settings.memory": "记忆",
      "settings.schedule": "定时任务",
      "settings.default": "默认",
      "settings.yes": "是",
      "settings.no": "否",
      "settings.field.id": "ID",
      "settings.field.name": "名称",
      "settings.field.platform": "平台",
      "settings.field.engine": "引擎",
      "settings.field.model": "模型",
      "settings.field.workspace": "工作区",
      "settings.field.workspaceMode": "工作区模式",
      "settings.field.dataDir": "数据目录",
      "settings.field.adminUsers": "管理员数",
      "settings.field.allowedChats": "允许的会话数",
      "settings.field.enabled": "启用",
      "settings.field.defaultScope": "默认 scope",
      "settings.field.maxItems": "最大条数",
      "settings.field.maxChars": "最大字符数",
      "settings.field.autoLearnFailures": "自动学习失败",
      "settings.field.timezone": "时区",
      "settings.field.historyLimit": "历史保留数",
      "settings.field.maxConcurrent": "最大并发",
      "settings.field.jitter": "随机延迟",
      "settings.field.timeout": "超时",
      "settings.configPath": "配置文件：{path}。控制台不修改配置文件；修改后请重启服务生效。",
      "settings.configNotice": "控制台不修改配置文件；修改后请重启服务生效。",
      "error.loginExpired": "登录已过期",
      "error.requestFailed": "请求失败",
      "toast.refreshed": "已刷新",
    },
  };

  const state = {
    overview: null,
    bot: "",
    view: "overview",
    lang: "en",
    memories: [],
    memoryCounts: {},
    schedules: [],
    settings: null,
    targets: [],
    editingMemory: null,
    editingSchedule: null,
    schedulePreviewRuns: null,
    toastTimer: null,
  };

  const $ = (selector) => document.querySelector(selector);
  const $$ = (selector) => Array.from(document.querySelectorAll(selector));

  function t(key, params = {}) {
    const table = I18N[state.lang] || I18N.en;
    let value = table[key] ?? I18N.en[key] ?? key;
    for (const [name, replacement] of Object.entries(params)) {
      value = value.split(`{${name}}`).join(String(replacement));
    }
    return value;
  }

  function readStoredLanguage() {
    try {
      return localStorage.getItem("nectar:lang") === "zh" ? "zh" : "en";
    } catch {
      return "en";
    }
  }

  function memoryStatusLabel(status) {
    return ["candidate", "active", "archived"].includes(status)
      ? t(`memory.status.${status}`)
      : status;
  }

  function scheduleStatusLabel(status) {
    return ["active", "paused", "completed", "failed"].includes(status)
      ? t(`schedule.status.${status}`)
      : status;
  }

  function applyStaticTranslations() {
    document.documentElement.lang = state.lang;
    for (const node of $$("[data-i18n]")) {
      node.textContent = t(node.dataset.i18n);
    }
    for (const node of $$("[data-i18n-placeholder]")) {
      node.placeholder = t(node.dataset.i18nPlaceholder);
    }
    for (const node of $$("[data-i18n-aria-label]")) {
      node.setAttribute("aria-label", t(node.dataset.i18nAriaLabel));
    }
  }

  function applyLanguage(lang, persist = true) {
    state.lang = lang === "zh" ? "zh" : "en";
    if (persist) {
      try {
        localStorage.setItem("nectar:lang", state.lang);
      } catch {
        // Ignore storage failures in private browsing modes.
      }
    }
    const select = $("#languageSelect");
    if (select) select.value = state.lang;
    applyStaticTranslations();
    renderCurrentView();
    if ($("#scheduleDialog")?.open) {
      $("#scheduleDialogTitle").textContent = state.editingSchedule
        ? t("schedule.dialog.edit")
        : t("schedule.dialog.create");
      if (state.schedulePreviewRuns !== null) {
        renderSchedulePreview(state.schedulePreviewRuns);
      }
    }
  }

  function renderCurrentView() {
    $("#pageTitle").textContent = t(`page.${state.view}`);
    if (state.view === "overview") {
      renderOverview();
    } else if (state.view === "memory") {
      renderMemories();
    } else if (state.view === "schedules") {
      renderSchedules();
    } else if (state.view === "settings" && state.settings) {
      renderSettings(state.settings);
    }
  }

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined && text !== null) node.textContent = String(text);
    return node;
  }

  function clear(node) {
    while (node.firstChild) node.removeChild(node.firstChild);
  }

  function chip(text, className) {
    return el("span", `chip ${className || ""}`.trim(), text);
  }

  function button(label, className, onClick) {
    const node = el("button", `button button-small ${className || ""}`.trim(), label);
    node.type = "button";
    node.addEventListener("click", onClick);
    return node;
  }

  function readCookie(name) {
    const prefix = `${name}=`;
    for (const part of document.cookie.split(";")) {
      const value = part.trim();
      if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
    }
    return "";
  }

  async function api(path, options = {}) {
    const headers = { Accept: "application/json", ...(options.headers || {}) };
    if (options.body !== undefined && !headers["Content-Type"]) {
      headers["Content-Type"] = "application/json";
    }
    if (options.method && options.method !== "GET") {
      const csrf = readCookie("nectar_csrf");
      if (csrf) headers["X-CSRF-Token"] = csrf;
    }
    const response = await fetch(path, {
      credentials: "same-origin",
      ...options,
      headers,
    });
    if (response.status === 401) {
      window.location.href = "/login";
      throw new Error(t("error.loginExpired"));
    }
    const text = await response.text();
    let body = null;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        body = { error: text };
      }
    }
    if (!response.ok) {
      throw new Error((body && body.error) || response.statusText || t("error.requestFailed"));
    }
    return body;
  }

  function toast(message, isError = false) {
    const node = $("#toast");
    node.textContent = message;
    node.classList.toggle("is-error", isError);
    node.hidden = false;
    window.clearTimeout(state.toastTimer);
    state.toastTimer = window.setTimeout(() => {
      node.hidden = true;
    }, 3600);
  }

  function fmtTime(value) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime()) || date.getUTCFullYear() < 2000) return "—";
    return date.toLocaleString(state.lang === "zh" ? "zh-CN" : "en-US", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  function fmtUptime(seconds) {
    const total = Math.max(0, Number(seconds) || 0);
    const days = Math.floor(total / 86400);
    const hours = Math.floor((total % 86400) / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    if (days) return `${days}d ${hours}h`;
    if (hours) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  }

  function botData(id) {
    if (!state.overview) return null;
    return (state.overview.bots || []).find((bot) => bot.id === id) || null;
  }

  function botPath(suffix) {
    return `/api/v1/bots/${encodeURIComponent(state.bot)}${suffix}`;
  }

  function updateBotSelect() {
    const select = $("#botSelect");
    const previous = state.bot;
    clear(select);
    const bots = (state.overview && state.overview.bots) || [];
    for (const bot of bots) {
      const option = el("option", "", `${bot.name} · ${bot.id}`);
      option.value = bot.id;
      select.append(option);
    }
    if (!bots.some((bot) => bot.id === previous)) {
      state.bot = bots.length ? bots[0].id : "";
    }
    select.value = state.bot;
    select.disabled = bots.length <= 1;
  }

  function setReadOnly() {
    const badge = $("#readOnlyBadge");
    badge.hidden = !(state.overview && state.overview.read_only);
    const newSchedule = $("#newScheduleBtn");
    if (newSchedule) newSchedule.disabled = isReadOnly();
  }

  function isReadOnly() {
    return Boolean(state.overview && state.overview.read_only);
  }

  async function loadOverview() {
    state.overview = await api("/api/v1/overview");
    updateBotSelect();
    setReadOnly();
    $("#versionLabel").textContent = state.overview.version || "dev";
    if (state.view === "overview") renderOverview();
  }

  function summaryCard(value, label, hint) {
    const card = el("div", "summary-card");
    card.append(el("div", "summary-value", value));
    card.append(el("div", "summary-label", label));
    if (hint) card.append(el("div", "summary-hint", hint));
    return card;
  }

  function renderOverview() {
    const overview = state.overview;
    if (!overview) return;
    const bots = overview.bots || [];
    const activeMemories = bots.reduce((sum, bot) => sum + ((bot.memory_counts || {}).active || 0), 0);
    const candidates = bots.reduce((sum, bot) => sum + ((bot.memory_counts || {}).candidate || 0), 0);
    const activeSchedules = bots.reduce((sum, bot) => sum + ((bot.schedule_counts || {}).active || 0), 0);
    const running = bots.reduce((sum, bot) => sum + (bot.running_schedules || 0), 0);

    const summary = $("#summaryCards");
    clear(summary);
    summary.append(summaryCard(
      bots.length,
      t("overview.summary.bots"),
      t("overview.summary.botsHint", { uptime: fmtUptime(overview.uptime_seconds) }),
    ));
    summary.append(summaryCard(
      activeMemories,
      t("overview.summary.activeMemories"),
      t("overview.summary.pendingMemories", { count: candidates }),
    ));
    summary.append(summaryCard(
      activeSchedules,
      t("overview.summary.activeSchedules"),
      running
        ? t("overview.summary.running", { count: running })
        : t("overview.summary.noRunning"),
    ));
    summary.append(summaryCard(
      overview.read_only ? t("overview.summary.readOnly") : t("overview.summary.writable"),
      t("overview.summary.mode"),
      overview.config_path || t("overview.summary.noConfigPath"),
    ));

    const cards = $("#botCards");
    clear(cards);
    for (const bot of bots) {
      cards.append(renderBotCard(bot));
    }

    const errorList = $("#errorsList");
    clear(errorList);
    let errorCount = 0;
    for (const bot of bots) {
      for (const message of [bot.memory_error, bot.schedule_error, bot.last_error]) {
        if (!message) continue;
        errorCount++;
        errorList.append(el("div", "error-item", `${bot.name}: ${message}`));
      }
    }
    if (!errorCount) errorList.append(el("div", "empty", t("overview.noErrors")));
    $("#errorsMeta").textContent = t("overview.runtimes", { count: bots.length });
  }

  function renderBotCard(bot) {
    const card = el("article", "bot-card");
    const head = el("div", "bot-card-head");
    const ident = el("div", "bot-ident");
    const dot = el("span", "status-dot");
    if (bot.memory_error || bot.schedule_error) dot.classList.add("is-error");
    else if (bot.last_error) dot.classList.add("is-warn");
    ident.append(dot);
    const text = el("div");
    text.append(el("div", "bot-name", bot.name));
    text.append(el("div", "bot-sub", `${bot.platform} · ${bot.engine} · ${bot.model || t("overview.defaultModel")}`));
    ident.append(text);
    head.append(ident);
    head.append(el("div", "bot-id", bot.id));
    card.append(head);

    const metrics = el("div", "bot-metrics");
    const counts = bot.memory_counts || {};
    const schedules = bot.schedule_counts || {};
    metrics.append(metric(counts.active || 0, t("overview.metric.activeMemory")));
    metrics.append(metric(counts.candidate || 0, t("overview.metric.pendingMemory")));
    metrics.append(metric(schedules.active || 0, t("overview.metric.activeSchedule")));
    card.append(metrics);

    const paths = el("div", "bot-path");
    paths.append(pathRow(t("overview.path.workspace"), bot.workspace || "—"));
    paths.append(pathRow(t("overview.path.data"), bot.data_dir || "—"));
    if (bot.next_run) paths.append(pathRow(t("overview.path.nextRun"), fmtTime(bot.next_run)));
    card.append(paths);
    return card;
  }

  function metric(value, label) {
    const node = el("div", "metric");
    node.append(el("div", "metric-value", value));
    node.append(el("div", "metric-label", label));
    return node;
  }

  function pathRow(label, value) {
    const row = el("div", "path-row");
    row.append(el("span", "", label));
    row.append(el("code", "", value));
    return row;
  }

  async function loadMemories() {
    if (!state.bot) return;
    const params = new URLSearchParams();
    const status = $("#memoryStatus").value;
    const scope = $("#memoryScope").value;
    const kind = $("#memoryKind").value;
    const query = $("#memorySearch").value.trim();
    params.set("status", status);
    params.set("scope", scope);
    params.set("kind", kind);
    if (query) params.set("q", query);
    const data = await api(botPath(`/memories?${params.toString()}`));
    state.memories = data.memories || [];
    state.memoryCounts = data.counts || {};
    renderMemories();
  }

  function renderMemories() {
    const counts = state.memoryCounts || {};
    const countRow = $("#memoryCounts");
    clear(countRow);
    for (const [label, key, cls] of [
      [t("memory.count.pending"), "candidate", "chip-candidate"],
      [t("memory.count.active"), "active", "chip-active"],
      [t("memory.count.archived"), "archived", "chip-archived"],
    ]) {
      const pill = el("span", "count-pill");
      pill.append(document.createTextNode(`${label} `));
      pill.append(el("strong", "", counts[key] || 0));
      countRow.append(pill);
    }
    $("#memoryMeta").textContent = t("memory.results", { count: state.memories.length });

    const list = $("#memoryList");
    clear(list);
    if (!state.memories.length) {
      list.append(el("div", "empty", t("memory.empty")));
      return;
    }
    for (const item of state.memories) {
      list.append(renderMemoryCard(item));
    }
  }

  function renderMemoryCard(item) {
    const card = el("article", "memory-card");
    const head = el("div", "memory-head");
    const meta = el("div", "memory-meta");
    meta.append(chip(item.kind || "fact"));
    meta.append(chip(item.scope || "chat"));
    meta.append(chip(memoryStatusLabel(item.status || "candidate"), `chip-${item.status || "candidate"}`));
    if (item.pinned) meta.append(chip(t("memory.pinned"), "chip-pinned"));
    if (item.locality) meta.append(chip(item.locality));
    head.append(meta);

    const content = el("div", "memory-content", item.content || "");
    card.append(head);
    card.append(content);

    const sub = el("div", "muted",
      [
        item.scope_id,
        t("memory.used", { count: item.use_count || 0 }),
        t("memory.updated", { time: fmtTime(item.updated_at) }),
      ]
        .filter(Boolean)
        .join(" · "));
    card.append(sub);

    if (!isReadOnly()) {
      const actions = el("div", "memory-actions");
      if (item.status !== "active") {
        actions.append(button(t("common.approve"), "button-accent", () => patchMemory(item.id, { status: "active" })));
      }
      if (item.status !== "archived") {
        actions.append(button(t("common.archive"), "button-ghost", () => patchMemory(item.id, { status: "archived" })));
      }
      actions.append(button(item.pinned ? t("common.unpin") : t("common.pin"), "button-ghost", () => patchMemory(item.id, { pinned: !item.pinned })));
      actions.append(button(t("common.edit"), "button-ghost", () => openMemoryDialog(item)));
      actions.append(button(t("common.delete"), "button-danger", () => deleteMemory(item)));
      card.append(actions);
    }
    return card;
  }

  async function patchMemory(id, patch) {
    try {
      await api(botPath(`/memories/${encodeURIComponent(id)}`), {
        method: "PATCH",
        body: JSON.stringify(patch),
      });
      toast(t("memory.toast.updated"));
      await Promise.all([loadMemories(), loadOverview()]);
    } catch (error) {
      toast(error.message, true);
    }
  }

  async function deleteMemory(item) {
    if (!window.confirm(t("memory.confirmDelete"))) return;
    try {
      await api(botPath(`/memories/${encodeURIComponent(item.id)}?confirm=1`), { method: "DELETE" });
      toast(t("memory.toast.deleted"));
      await Promise.all([loadMemories(), loadOverview()]);
    } catch (error) {
      toast(error.message, true);
    }
  }

  function openMemoryDialog(item) {
    state.editingMemory = item;
    $("#memoryDialogMeta").textContent = `${item.kind} · ${item.scope} · ${item.id}`;
    $("#memoryContent").value = item.content || "";
    $("#memoryDialog").showModal();
  }

  async function saveMemory() {
    const item = state.editingMemory;
    if (!item) return;
    try {
      await api(botPath(`/memories/${encodeURIComponent(item.id)}`), {
        method: "PATCH",
        body: JSON.stringify({ content: $("#memoryContent").value }),
      });
      $("#memoryDialog").close();
      toast(t("memory.toast.saved"));
      await loadMemories();
    } catch (error) {
      toast(error.message, true);
    }
  }

  async function loadTargets() {
    if (!state.bot) return [];
    const data = await api(botPath("/targets"));
    state.targets = data.targets || [];
    return state.targets;
  }

  async function loadSchedules() {
    if (!state.bot) return;
    const showAll = $("#showAllSchedules").checked;
    const data = await api(botPath(`/schedules?all=${showAll ? "1" : "0"}`));
    state.schedules = data.tasks || [];
    renderSchedules();
  }

  function renderSchedules() {
    $("#scheduleMeta").textContent = t("schedule.count", { count: state.schedules.length });
    const list = $("#scheduleList");
    clear(list);
    if (!state.schedules.length) {
      list.append(el("div", "empty", t("schedule.empty")));
      return;
    }
    for (const task of state.schedules) {
      list.append(renderScheduleCard(task));
    }
  }

  function renderScheduleCard(task) {
    const card = el("article", `schedule-card${task.running ? " is-running" : ""}`);
    const head = el("div", "schedule-head");
    const title = el("div");
    const titleText = task.description || firstLine(task.prompt) || task.id;
    title.append(el("div", "schedule-title", titleText));
    title.append(el("div", "muted", `${task.mode} · ${task.id}`));
    head.append(title);

    const statusWrap = el("div", "memory-meta");
    statusWrap.append(chip(scheduleStatusLabel(task.status || "active"), `chip-${task.status || "active"}`));
    if (task.running) statusWrap.append(chip(t("schedule.running"), "chip-active"));
    head.append(statusWrap);
    card.append(head);

    card.append(el("div", "schedule-prompt", task.prompt || ""));

    const facts = el("div", "schedule-facts");
    facts.append(fact(t("schedule.fact.next"), fmtTime(task.next_run)));
    facts.append(fact(t("schedule.fact.last"), fmtTime(task.last_run)));
    facts.append(fact(t("schedule.fact.runs"), task.run_count || 0));
    facts.append(fact(t("schedule.fact.target"), scheduleTarget(task)));
    if (task.timezone) facts.append(fact(t("schedule.fact.tz"), task.timezone));
    if (task.timeout) facts.append(fact(t("schedule.fact.timeout"), task.timeout));
    card.append(facts);

    if (task.last_error) card.append(el("div", "schedule-error", task.last_error));

    if (!isReadOnly()) {
      const actions = el("div", "schedule-actions");
      if (task.status === "active") {
        actions.append(button(t("schedule.action.pause"), "button-ghost", () => scheduleAction(task.id, "pause")));
        actions.append(button(t("schedule.action.run"), "button-ghost", () => scheduleAction(task.id, "run")));
      } else if (task.status === "paused") {
        actions.append(button(t("schedule.action.resume"), "button-accent", () => scheduleAction(task.id, "resume")));
        actions.append(button(t("schedule.action.run"), "button-ghost", () => scheduleAction(task.id, "run")));
      }
      if (task.running) actions.append(button(t("schedule.action.cancel"), "button-danger", () => scheduleAction(task.id, "cancel")));
      actions.append(button(t("schedule.action.edit"), "button-ghost", () => openScheduleDialog(task)));
      actions.append(button(t("schedule.action.delete"), "button-danger", () => deleteSchedule(task)));
      card.append(actions);
    }
    return card;
  }

  function firstLine(text) {
    return String(text || "").split("\n")[0].slice(0, 80);
  }

  function fact(label, value) {
    const node = el("span", "fact");
    node.append(el("span", "", `${label}:`));
    node.append(document.createTextNode(String(value)));
    return node;
  }

  function scheduleTarget(task) {
    const key = task.session_key || {};
    const channel = key.channel || key.Channel || "";
    const chat = key.chat_id || key.ChatID || "";
    const thread = key.thread_id || key.ThreadID || "";
    return [channel, chat, thread].filter(Boolean).join(" / ") || "—";
  }

  async function scheduleAction(id, action) {
    try {
      await api(botPath(`/schedules/${encodeURIComponent(id)}/${encodeURIComponent(action)}`), { method: "POST" });
      toast(t("schedule.toast.updated"));
      await Promise.all([loadSchedules(), loadOverview()]);
    } catch (error) {
      toast(error.message, true);
    }
  }

  async function deleteSchedule(task) {
    if (!window.confirm(t("schedule.confirmDelete", { name: task.description || task.id }))) return;
    try {
      await api(botPath(`/schedules/${encodeURIComponent(task.id)}`), { method: "DELETE" });
      toast(t("schedule.toast.deleted"));
      await Promise.all([loadSchedules(), loadOverview()]);
    } catch (error) {
      toast(error.message, true);
    }
  }

  async function openScheduleDialog(task) {
    if (isReadOnly()) {
      toast(t("schedule.readonly"), true);
      return;
    }
    state.editingSchedule = task || null;
    state.schedulePreviewRuns = null;
    $("#scheduleDialogTitle").textContent = task
      ? t("schedule.dialog.edit")
      : t("schedule.dialog.create");
    $("#schedulePreview").textContent = "";
    await loadTargets();

    const key = task ? (task.session_key || {}) : {};
    const targetValue = task
      ? [
          key.channel || key.Channel || "",
          key.chat_id || key.ChatID || "",
          key.thread_id || key.ThreadID || "",
        ].join("|")
      : "";

    const select = $("#scheduleTarget");
    clear(select);
    if (!state.targets.length) {
      const option = el("option", "", t("schedule.noTargets"));
      option.value = "";
      select.append(option);
    } else {
      for (const target of state.targets) {
        const option = el("option", "", target.label);
        option.value = [target.channel, target.chat_id, target.thread_id || ""].join("|");
        select.append(option);
      }
    }
    if (task && targetValue && !Array.from(select.options).some((option) => option.value === targetValue)) {
      const option = el("option", "", `${key.channel || key.Channel || ""} / ${key.chat_id || key.ChatID || ""}`);
      option.value = targetValue;
      select.append(option);
    }
    select.disabled = Boolean(task);

    $("#scheduleMode").value = (task && task.mode) || "cron";
    $("#scheduleCron").value = (task && task.cron) || "";
    $("#scheduleEvery").value = (task && task.every) || "";
    $("#scheduleIn").value = "";
    $("#scheduleAt").value = task && task.mode === "once" ? (task.execute_at || "") : "";
    $("#scheduleTimezone").value = (task && task.timezone) || "";
    $("#scheduleTimeout").value = (task && task.timeout) || "";
    $("#scheduleDesc").value = (task && task.description) || "";
    $("#schedulePrompt").value = (task && task.prompt) || "";
    updateScheduleModeFields();
    $("#scheduleDialog").showModal();
    if (targetValue) {
      select.value = targetValue;
    } else if (select.options.length) {
      select.value = select.options[0].value;
    }
  }

  function updateScheduleModeFields() {
    const mode = $("#scheduleMode").value;
    for (const field of $$("[data-mode-field]")) {
      field.hidden = field.dataset.modeField !== mode;
    }
  }

  function scheduleRequest() {
    const mode = $("#scheduleMode").value;
    const request = {
      mode,
      prompt: $("#schedulePrompt").value,
      description: $("#scheduleDesc").value,
      timezone: $("#scheduleTimezone").value,
      timeout: $("#scheduleTimeout").value,
    };
    if (mode === "cron") request.cron = $("#scheduleCron").value;
    if (mode === "every") request.every = $("#scheduleEvery").value;
    if (mode === "in") request.in = $("#scheduleIn").value;
    if (mode === "at") request.at = $("#scheduleAt").value;
    if (!state.editingSchedule) {
      const [channel, chatID, threadID] = $("#scheduleTarget").value.split("|");
      request.channel = channel || "";
      request.chat_id = chatID || "";
      request.thread_id = threadID || "";
    }
    return request;
  }

  async function previewSchedule() {
    try {
      const data = await api(botPath("/schedules/preview"), {
        method: "POST",
        body: JSON.stringify(scheduleRequest()),
      });
      const runs = data.next_runs || [];
      state.schedulePreviewRuns = runs;
      renderSchedulePreview(runs);
    } catch (error) {
      state.schedulePreviewRuns = null;
      $("#schedulePreview").textContent = error.message;
    }
  }

  function renderSchedulePreview(runs) {
    $("#schedulePreview").textContent = runs.length
      ? t("schedule.preview.next", { runs: runs.map(fmtTime).join("  ·  ") })
      : t("schedule.preview.none");
  }

  async function saveSchedule() {
    try {
      if (state.editingSchedule) {
        await api(botPath(`/schedules/${encodeURIComponent(state.editingSchedule.id)}`), {
          method: "PATCH",
          body: JSON.stringify(scheduleRequest()),
        });
      } else {
        await api(botPath("/schedules"), {
          method: "POST",
          body: JSON.stringify(scheduleRequest()),
        });
      }
      $("#scheduleDialog").close();
      toast(state.editingSchedule ? t("schedule.toast.saved") : t("schedule.toast.created"));
      await Promise.all([loadSchedules(), loadOverview()]);
    } catch (error) {
      toast(error.message, true);
    }
  }

  async function loadSettings() {
    if (!state.bot) return;
    const data = await api(botPath("/settings"));
    state.settings = data;
    renderSettings(data);
  }

  function renderSettings(data) {
    const panel = $("#settingsPanel");
    clear(panel);
    panel.append(settingsCard(t("settings.runtime"), [
      [t("settings.field.id"), data.id],
      [t("settings.field.name"), data.name],
      [t("settings.field.platform"), data.platform],
      [t("settings.field.engine"), data.engine],
      [t("settings.field.model"), data.model || t("settings.default")],
      [t("settings.field.workspace"), data.workspace],
      [t("settings.field.workspaceMode"), data.workspace_mode],
      [t("settings.field.dataDir"), data.data_dir],
      [t("settings.field.adminUsers"), data.admin_users],
      [t("settings.field.allowedChats"), data.allowed_chats],
    ]));
    const memory = data.memory || {};
    panel.append(settingsCard(t("settings.memory"), [
      [t("settings.field.enabled"), yesNo(memory.enabled)],
      [t("settings.field.defaultScope"), memory.default_scope],
      [t("settings.field.maxItems"), memory.max_items],
      [t("settings.field.maxChars"), memory.max_chars],
      [t("settings.field.autoLearnFailures"), yesNo(memory.auto_learn_failures)],
    ]));
    const schedule = data.schedule || {};
    panel.append(settingsCard(t("settings.schedule"), [
      [t("settings.field.enabled"), yesNo(schedule.enabled)],
      [t("settings.field.timezone"), schedule.timezone],
      [t("settings.field.historyLimit"), schedule.history_limit],
      [t("settings.field.maxConcurrent"), schedule.max_concurrent],
      [t("settings.field.jitter"), schedule.jitter],
      [t("settings.field.timeout"), schedule.timeout],
    ]));
    $("#configNotice").textContent = state.overview && state.overview.config_path
      ? t("settings.configPath", { path: state.overview.config_path })
      : t("settings.configNotice");
  }

  function yesNo(value) {
    return value ? t("settings.yes") : t("settings.no");
  }

  function settingsCard(title, rows) {
    const card = el("div", "settings-card");
    card.append(el("h3", "", title));
    const kv = el("div", "kv");
    for (const [key, value] of rows) {
      const row = el("div", "kv-row");
      row.append(el("span", "kv-key", key));
      row.append(el("span", "kv-value", value === undefined || value === null || value === "" ? "—" : value));
      kv.append(row);
    }
    card.append(kv);
    return card;
  }

  function showView(view) {
    state.view = view;
    for (const item of $$(".nav-item")) {
      item.classList.toggle("is-active", item.dataset.view === view);
    }
    for (const section of $$(".view")) {
      section.hidden = section.id !== `view-${view}`;
    }
    $("#pageTitle").textContent = t(`page.${view}`);
    if (view === "overview") renderOverview();
    if (view === "memory") loadMemories().catch((error) => toast(error.message, true));
    if (view === "schedules") loadSchedules().catch((error) => toast(error.message, true));
    if (view === "settings") loadSettings().catch((error) => toast(error.message, true));
  }

  function bindEvents() {
    for (const item of $$(".nav-item")) {
      item.addEventListener("click", () => {
        history.replaceState(null, "", `#${item.dataset.view}`);
        showView(item.dataset.view);
      });
    }
    $("#botSelect").addEventListener("change", (event) => {
      state.bot = event.target.value;
      showView(state.view);
    });
    $("#languageSelect").addEventListener("change", (event) => {
      applyLanguage(event.target.value);
    });
    $("#refreshBtn").addEventListener("click", async () => {
      try {
        await loadOverview();
        await Promise.all([
          state.view === "memory" ? loadMemories() : Promise.resolve(),
          state.view === "schedules" ? loadSchedules() : Promise.resolve(),
          state.view === "settings" ? loadSettings() : Promise.resolve(),
        ]);
        toast(t("toast.refreshed"));
      } catch (error) {
        toast(error.message, true);
      }
    });
    $("#memoryRefresh").addEventListener("click", () => loadMemories().catch((error) => toast(error.message, true)));
    $("#memorySearch").addEventListener("keydown", (event) => {
      if (event.key === "Enter") loadMemories().catch((error) => toast(error.message, true));
    });
    for (const id of ["#memoryStatus", "#memoryScope", "#memoryKind"]) {
      $(id).addEventListener("change", () => loadMemories().catch((error) => toast(error.message, true)));
    }
    $("#showAllSchedules").addEventListener("change", () => loadSchedules().catch((error) => toast(error.message, true)));
    $("#newScheduleBtn").addEventListener("click", () => openScheduleDialog(null).catch((error) => toast(error.message, true)));
    $("#scheduleMode").addEventListener("change", updateScheduleModeFields);
    $("#schedulePreviewBtn").addEventListener("click", previewSchedule);
    $("#scheduleSaveBtn").addEventListener("click", saveSchedule);
    $("#memorySaveBtn").addEventListener("click", saveMemory);
    for (const node of $$("[data-close-dialog]")) {
      node.addEventListener("click", () => {
        const dialog = document.getElementById(node.dataset.closeDialog);
        if (dialog) dialog.close();
      });
    }
    window.addEventListener("hashchange", () => {
      const view = location.hash.replace("#", "");
      if (["overview", "memory", "schedules", "settings"].includes(view)) showView(view);
    });
  }

  async function boot() {
    state.lang = readStoredLanguage();
    applyLanguage(state.lang, false);
    bindEvents();
    try {
      await loadOverview();
      const initial = location.hash.replace("#", "");
      showView(["overview", "memory", "schedules", "settings"].includes(initial) ? initial : "overview");
    } catch (error) {
      toast(error.message, true);
    }
    window.setInterval(() => {
      if (document.hidden) return;
      loadOverview().catch(() => {});
    }, 15000);
  }

  document.addEventListener("DOMContentLoaded", boot);
})();
