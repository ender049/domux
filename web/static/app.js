const endpoints = {
  applications: '/api/v1/applications',
  customApps: '/api/v1/apps',
  zones: '/api/v1/zones',
  providers: '/api/v1/ddns-providers',
  providerCatalog: '/api/v1/ddns-providers/catalog',
  sources: '/api/v1/runtimes',
  routes: '/api/v1/routes',
  agents: '/api/v1/agents',
  resources: '/api/v1/nodes/resources',
  pendingAgents: '/api/v1/agents/pending',
  ddns: '/api/v1/ddns',
  targets: '/api/v1/deploy-targets',
  certificates: '/api/v1/certificates',
  deployments: '/api/v1/deployments',
  jobs: '/api/v1/jobs',
  logs: '/api/v1/logs',
};

const actionEndpoints = {
  routesRefresh: '/api/v1/actions/routes/refresh',
  ddnsSync: '/api/v1/actions/ddns/sync',
  certRenew: '/api/v1/actions/certificates/renew',
  certDeploy: '/api/v1/actions/certificates/deploy',
};

const state = {
  activeWorkspace: 'applications',
  appFilter: { type: 'all' },
  selectedAppID: '',
  loading: false,
  polling: {
    resources: 0,
    data: 0,
    resourcesBusy: false,
    dataBusy: false,
  },
  editing: {},
  toastTimer: 0,
  data: {
    applications: [],
    customApps: [],
    zones: [],
    providers: [],
    providerCatalog: [],
    sources: [],
    routes: [],
    agents: [],
    resources: [],
    pendingAgents: [],
    ddns: [],
    targets: [],
    certificates: [],
    deployments: [],
    jobs: [],
    logs: [],
  },
};

const statusCopy = {
  proxied: '已代理',
  unproxied: '未代理',
  success: '正常',
  failed: '失败',
  noop: '正常',
  signing: '待签发',
  expiring: '将过期',
  running: '运行中',
  online: '在线',
  offline: '离线',
};

const originCopy = {
  local_docker: 'Docker 发现',
  remote_agent: '节点发现',
  custom_app: '自定义',
};

const runtimeCopy = {
  docker: 'Docker',
  podman: 'Podman',
};

const transportCopy = {
  local: '本地',
  agent: '节点',
  ssh: 'SSH',
};

document.addEventListener('DOMContentLoaded', () => {
  restoreTheme();
  bindNavigation();
  bindGlobalActions();
  bindDialogs();
  bindForms();
  window.addEventListener('hashchange', restoreWorkspaceFromHash);
  window.addEventListener('popstate', restoreWorkspaceFromHash);
  document.addEventListener('visibilitychange', handleVisibilityChange);
  restoreWorkspaceFromHash();
  loadAll().then(startPolling);
});

async function loadAll(options = {}) {
  const quiet = Boolean(options.quiet);
  if (!quiet) {
    state.loading = true;
    renderLoading();
  }
  const entries = Object.entries(endpoints);
  const results = await Promise.allSettled(entries.map(([, url]) => fetchJSON(url)));
  const failures = [];
  results.forEach((result, index) => {
    const [key] = entries[index];
    if (result.status === 'fulfilled') {
      state.data[key] = key === 'logs' ? result.value : Array.isArray(result.value) ? result.value : [];
    } else {
      state.data[key] = key === 'logs' ? { text: '' } : [];
      failures.push(result.reason.message || key);
    }
  });
  state.loading = false;
  syncSelectedApp();
  populateSelects();
  renderAll();
  if (!quiet && failures.length > 0) {
    showToast(`部分数据加载失败：${failures.slice(0, 2).join('；')}`, 'bad');
  }
}

async function refreshResources() {
  if (state.polling.resourcesBusy || document.hidden) return;
  state.polling.resourcesBusy = true;
  try {
    const resources = await fetchJSON(endpoints.resources);
    state.data.resources = Array.isArray(resources) ? resources : [];
    renderOverview();
    if (state.activeWorkspace === 'nodes') {
    renderNodes();
    renderLogs();
    }
  } catch {
    // Keep the last known resource snapshot during transient failures.
  } finally {
    state.polling.resourcesBusy = false;
  }
}

async function refreshDataQuietly() {
  if (state.polling.dataBusy || document.hidden) return;
  state.polling.dataBusy = true;
  try {
    await loadAll({ quiet: true });
  } finally {
    state.polling.dataBusy = false;
  }
}

function startPolling() {
  stopPolling();
  if (document.hidden) return;
  state.polling.resources = window.setInterval(refreshResources, 5000);
  state.polling.data = window.setInterval(refreshDataQuietly, 30000);
}

function stopPolling() {
  if (state.polling.resources) {
    window.clearInterval(state.polling.resources);
    state.polling.resources = 0;
  }
  if (state.polling.data) {
    window.clearInterval(state.polling.data);
    state.polling.data = 0;
  }
}

function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling();
    return;
  }
  refreshResources();
  refreshDataQuietly();
  startPolling();
}

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, {
    cache: 'no-store',
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    ...options,
  });
  let payload = null;
  const text = await response.text();
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch (error) {
      throw new Error(`invalid response from ${url}`);
    }
  }
  if (!response.ok) {
    throw new Error(localizeError((payload && (payload.error || payload.message)) || response.statusText));
  }
  return payload;
}

function bindNavigation() {
  for (const target of document.querySelectorAll('[data-nav-target]')) {
    target.addEventListener('click', (event) => {
      event.preventDefault();
      showWorkspace(target.dataset.navTarget);
      if (target.dataset.navTarget === 'applications') {
        renderApplications();
      }
    });
  }
}

function bindGlobalActions() {
  document.getElementById('refresh-all').addEventListener('click', () => loadAll());
  document.getElementById('theme-toggle').addEventListener('click', toggleTheme);
  document.addEventListener('click', (event) => {
    const appFilter = event.target.closest('[data-app-filter]');
    if (appFilter) {
      state.appFilter = { type: appFilter.dataset.appFilter };
      renderApplications();
      return;
    }
    const opener = event.target.closest('[data-open-dialog]');
    if (opener) {
      if (opener.dataset.openDialog === 'custom-app') {
        state.appFilter = { type: 'all' };
      }
      openDialog(opener.dataset.openDialog, opener.dataset.dialogId || '');
      return;
    }
    const close = event.target.closest('[data-close-dialog]');
    if (close) {
      close.closest('dialog')?.close();
      return;
    }
    const action = event.target.closest('[data-action]');
    if (action) {
      handleAction(action.dataset.action, action.dataset);
    }
  });
}

function bindDialogs() {
  for (const dialog of document.querySelectorAll('dialog')) {
    dialog.addEventListener('click', (event) => {
      if (event.target === dialog) {
        dialog.close();
      }
    });
  }
}

function bindForms() {
  document.getElementById('custom-app-form').addEventListener('submit', submitCustomApp);
  document.getElementById('zone-form').addEventListener('submit', submitZone);
  document.getElementById('provider-form').addEventListener('submit', submitProvider);
  document.getElementById('agent-form').addEventListener('submit', submitAgent);
  document.getElementById('deploy-target-form').addEventListener('submit', submitDeployTarget);
  document.getElementById('deploy-target-transport').addEventListener('change', updateDeployTargetTransportFields);
  document.getElementById('agent-runtime').addEventListener('change', updateAgentNetworkOptions);
  document.getElementById('agent-addr').addEventListener('blur', updateAgentNetworkOptions);
  document.getElementById('zone-ddns-provider').addEventListener('change', updateZoneProviderDependentFields);
  document.getElementById('zone-certificate-deploy-targets').addEventListener('change', updateDeployTargetSummary);
}

function showWorkspace(name, options = {}) {
  const nextWorkspace = ['applications', 'domains', 'nodes', 'logs'].includes(name) ? name : 'applications';
  const syncHash = options.syncHash !== false;
  state.activeWorkspace = nextWorkspace;
  for (const nav of document.querySelectorAll('[data-nav-target]')) {
    nav.classList.toggle('is-active', nav.dataset.navTarget === nextWorkspace);
    if (nav.getAttribute('role') === 'tab') {
      nav.setAttribute('aria-selected', String(nav.dataset.navTarget === nextWorkspace));
    }
  }
  for (const workspace of document.querySelectorAll('[data-workspace]')) {
    const active = workspace.dataset.workspace === nextWorkspace;
    workspace.hidden = !active;
    workspace.classList.toggle('is-active', active);
  }
  if (syncHash) {
    const nextHash = `#${nextWorkspace}`;
    if (window.location.hash !== nextHash) {
      history.pushState({ workspace: nextWorkspace }, '', nextHash);
    }
  }
  window.scrollTo({ top: 0, left: 0, behavior: 'auto' });
}

function restoreWorkspaceFromHash() {
  const hash = window.location.hash.replace('#', '').trim();
  if (hash === 'applications') {
    state.appFilter = { type: 'all' };
  }
  if (['applications', 'domains', 'nodes', 'logs'].includes(hash)) {
    showWorkspace(hash, { syncHash: false });
    return;
  }
  showWorkspace('applications', { syncHash: false });
}

function renderLoading() {
  const overview = document.getElementById('system-overview');
  overview.innerHTML = [0, 1, 2].map(() => `
    <div class="summary-item"><span>加载中</span><strong>--</strong></div>
  `).join('');
}

function renderAll() {
  renderOverview();
  renderApplications();
  renderDomains();
  renderNodes();
  renderLogs();
}

function renderOverview() {
  const apps = state.data.applications;
  const issues = apps.filter(isAppIssue).length;
  const zones = state.data.zones;
  const nodes = nodeItems();
  const ddnsIssues = ddnsProblemItems().length;
  const nodeIssues = nodes.filter((node) => isBadStatus(node.status)).length;
  const cards = [
    { id: 'applications', label: '应用', value: apps.length, tone: issues ? 'warning' : 'good' },
    { id: 'domains', label: '域名', value: zones.length, tone: ddnsIssues ? 'bad' : zones.length ? 'good' : 'warning' },
    { id: 'nodes', label: '节点', value: nodes.length, tone: nodeIssues ? 'bad' : nodes.length ? 'good' : 'warning' },
  ];
  document.getElementById('system-overview').innerHTML = cards.map((card) => `
    <button class="summary-item ${state.activeWorkspace === card.id ? 'is-active' : ''}" type="button" role="tab" aria-selected="${state.activeWorkspace === card.id ? 'true' : 'false'}" data-nav-target="${escapeAttribute(card.id)}" data-tone="${escapeAttribute(card.tone)}">
      <span>${escapeHTML(card.label)}</span>
      <strong>${escapeHTML(String(card.value))}</strong>
    </button>
  `).join('');
  for (const tab of document.querySelectorAll('#system-overview [data-nav-target]')) {
    tab.addEventListener('click', () => {
      if (tab.dataset.navTarget === 'applications') {
        state.appFilter = { type: 'all' };
      }
      showWorkspace(tab.dataset.navTarget);
      if (tab.dataset.navTarget === 'applications') {
        renderApplications();
      }
    });
  }
}

function renderApplications() {
  const apps = filteredApplications();
  const list = document.getElementById('application-list');
  renderAppFilters();
  if (apps.length === 0) {
    list.innerHTML = emptyState('没有匹配的应用', '容器带 domux.* 标签会出现在这里，也可以手动添加外部网站。');
  } else {
    list.innerHTML = apps.map(renderApplicationCard).join('');
  }
  for (const edit of list.querySelectorAll('[data-edit-custom-app]')) {
    edit.addEventListener('click', (event) => {
      event.stopPropagation();
      openDialog('custom-app', edit.dataset.editCustomApp);
    });
  }
  for (const remove of list.querySelectorAll('[data-delete-custom-app]')) {
    remove.addEventListener('click', (event) => {
      event.stopPropagation();
      deleteCustomAppByName(remove.dataset.deleteCustomApp);
    });
  }
  for (const card of list.querySelectorAll('[data-open-url]')) {
    card.addEventListener('click', (event) => {
      if (event.target.closest('button, a')) return;
      window.open(card.dataset.openUrl, '_blank', 'noreferrer');
    });
    card.addEventListener('keydown', (event) => {
      if ((event.key === 'Enter' || event.key === ' ') && !event.target.closest('button, a')) {
        event.preventDefault();
        window.open(card.dataset.openUrl, '_blank', 'noreferrer');
      }
    });
  }
}

function renderAppFilters() {
  const target = document.getElementById('app-filters');
  if (!target) return;
  const apps = state.data.applications;
  const cards = [
    { id: 'all', label: '全部', count: apps.length },
    { id: 'proxied', label: '已代理', count: apps.filter(isManagedApp).length },
    { id: 'unproxied', label: '未代理', count: apps.filter(isAppIssue).length },
    { id: 'discovered', label: '自动发现', count: apps.filter((app) => app.origin !== 'custom_app').length },
    { id: 'custom', label: '自定义', count: apps.filter((app) => app.origin === 'custom_app').length },
  ];
  if (appFilterType() === 'node' && state.appFilter.node) {
    cards.push({ id: 'node', label: displayNodeName(state.appFilter.node), count: apps.filter((app) => appBelongsToNode(app, state.appFilter.node)).length });
  }
  const active = appFilterType();
  target.innerHTML = cards.map((card) => `
    <button class="filter-chip ${active === card.id ? 'is-active' : ''}" type="button" role="tab" aria-selected="${active === card.id ? 'true' : 'false'}" data-app-filter="${escapeAttribute(card.id)}">
      <span>${escapeHTML(card.label)}</span><strong>${escapeHTML(String(card.count))}</strong>
    </button>
  `).join('');
}

function openNodeApplications(nodeName) {
  state.appFilter = { type: 'node', node: nodeName };
  showWorkspace('applications');
  renderApplications();
}

function renderApplicationCard(app) {
  const managed = isManagedApp(app);
  const tone = managed ? 'good' : 'warning';
  const subtitle = app.host || '未生成预期入口';
  const proxy = appProxyLabel(app);
  const source = appSourceLabel(app);
  const custom = app.origin === 'custom_app';
  const entryLabel = app.entry_url || app.host || '未生成';
  return `
    <article class="object-card ${app.entry_url ? 'is-clickable' : ''} ${managed ? 'is-proxied' : 'is-unproxied'}" ${app.entry_url ? `role="link" tabindex="0" data-open-url="${escapeAttribute(app.entry_url)}"` : ''} style="--card-accent: ${toneColor(tone)}">
      <div class="card-top">
        ${avatar(app.name, app.icon)}
        <div class="card-title">
          <strong>${escapeHTML(app.name || '未命名应用')}</strong>
          <span>${escapeHTML(subtitle)}</span>
        </div>
        <div class="card-side">
          <div class="card-badges">
            ${statusBadge(app.status, tone)}
            ${managed ? '<span class="managed-logo-badge" title="已代理"><img src="/logo.svg" alt="已代理"></span>' : ''}
          </div>
          ${custom ? renderCardActions('应用操作', [
            { icon: 'edit', label: '编辑应用', attrs: `data-edit-custom-app="${escapeAttribute(app.name)}"` },
            { icon: 'delete', label: '删除应用', attrs: `data-delete-custom-app="${escapeAttribute(app.name)}"`, danger: true },
          ]) : ''}
        </div>
      </div>
      <div class="card-lines">
        ${dataLine('来源', source)}
        ${dataLine(managed ? '入口' : '预期入口', entryLabel, managed ? app.entry_url || app.host : '入口尚未生效，仅供预期确认')}
        ${dataLine('代理出口', proxy)}
        ${dataLine('目标', app.target_url || '未确定')}
        ${!managed && app.reason ? dataLine('原因', app.reason) : ''}
      </div>
        ${!managed ? '' : '<div class="card-actions"><button class="mini-action" type="button" data-action="routes-refresh">刷新</button></div>'}
    </article>
  `;
}

function renderDomains() {
  const list = document.getElementById('domain-list');
  const zones = state.data.zones;
  list.innerHTML = zones.length ? zones.map(renderDomainCard).join('') : emptyState('还没有域名', '添加一个默认域名后，容器只写少量 domux.* 标签就能获得入口。');
  for (const edit of list.querySelectorAll('[data-edit-zone]')) {
    edit.addEventListener('click', () => openDialog('zone', edit.dataset.editZone));
  }
  for (const sync of list.querySelectorAll('[data-sync-zone]')) {
    sync.addEventListener('click', () => handleAction('ddns-sync', { zone: sync.dataset.syncZone }));
  }
  for (const remove of list.querySelectorAll('[data-delete-zone]')) {
    remove.addEventListener('click', () => deleteZoneByName(remove.dataset.deleteZone));
  }
}

function bindZoneEditButtons(container) {
  for (const edit of container.querySelectorAll('[data-edit-zone]')) {
    edit.addEventListener('click', () => openDialog('zone', edit.dataset.editZone));
  }
}

function renderDomainCard(zone) {
  const ddnsStates = state.data.ddns.filter((item) => item.zone === zone.name);
  const badDDNS = ddnsStates.find((item) => item.error || isBadStatus(item.status));
  const certs = certificateItems().filter((item) => item.zone === zone.name);
  const appCount = state.data.applications.filter((app) => app.zone === zone.name).length;
  const tone = badDDNS ? 'bad' : zone.wildcard || (zone.ddns && zone.ddns.enabled) || (zone.certificate && zone.certificate.enabled) ? 'good' : 'info';
  const latestDDNS = latestByTime(ddnsStates, 'synced_at');
  const latestCert = latestByTime(certs, 'not_after');
  const ddnsEnabled = Boolean(zone.ddns && zone.ddns.enabled);
  const latestDeploy = latestDeployRunForZone(zone, certs);
  return `
    <article class="object-card" style="--card-accent: ${toneColor(tone)}">
      <div class="card-top">
        ${countAvatar(appCount, '应用数量')}
        <div class="card-title"><strong>${escapeHTML(zone.domain)}</strong>${zoneFeatureLabel(zone)}</div>
        <div class="card-side">
          ${renderCardActions('域名操作', [
            { icon: 'edit', label: '编辑域名', attrs: `data-edit-zone="${escapeAttribute(zone.name)}"` },
            { icon: 'delete', label: '删除域名', attrs: `data-delete-zone="${escapeAttribute(zone.name)}"`, danger: true },
          ])}
        </div>
      </div>
      <div class="card-lines">
        ${dataLine('IP 地址', latestDDNS && latestDDNS.value ? latestDDNS.value : '未同步')}
        ${ddnsEnabled ? clickableDataLine('DDNS', ddnsSummary(latestDDNS, badDDNS), '点击同步 DDNS 解析', zone.name) : dataLine('DDNS', '未启用')}
        ${dataLine('证书', certificateSummary(zone, latestCert))}
        ${dataLine('部署', deploySummary(zone, latestDeploy))}
        ${badDDNS && badDDNS.error ? dataLine('原因', ddnsErrorHint(badDDNS)) : ''}
      </div>
    </article>
  `;
}

function renderNodes() {
  renderAgentInstallPanel();
  const list = document.getElementById('node-list');
  const nodes = nodeItems();
  list.innerHTML = nodes.length ? nodes.map(renderNodeCard).join('') : emptyState('还没有节点', '添加一台节点。');
  for (const card of list.querySelectorAll('[data-node-apps]')) {
    card.addEventListener('click', (event) => {
      if (event.target.closest('button, a')) return;
      openNodeApplications(card.dataset.nodeApps);
    });
    card.addEventListener('keydown', (event) => {
      if ((event.key === 'Enter' || event.key === ' ') && !event.target.closest('button, a')) {
        event.preventDefault();
        openNodeApplications(card.dataset.nodeApps);
      }
    });
  }
  for (const edit of list.querySelectorAll('[data-edit-agent]')) {
    edit.addEventListener('click', () => openDialog('agent', edit.dataset.editAgent));
  }
  for (const remove of list.querySelectorAll('[data-delete-agent]')) {
    remove.addEventListener('click', () => deleteAgentByName(remove.dataset.deleteAgent));
  }
  for (const approve of list.querySelectorAll('[data-approve-agent]')) {
    approve.addEventListener('click', () => approvePendingAgent(approve.dataset.approveAgent));
  }
  for (const reject of list.querySelectorAll('[data-reject-agent]')) {
    reject.addEventListener('click', () => rejectPendingAgent(reject.dataset.rejectAgent));
  }
}

function renderLogs() {
  const target = document.getElementById('system-log');
  if (!target) return;
  const text = state.data.logs && typeof state.data.logs.text === 'string' ? state.data.logs.text : '';
  target.textContent = text || '暂无日志';
  target.scrollTop = target.scrollHeight;
}

function renderAgentInstallPanel() {
  const panel = document.getElementById('agent-install-panel');
  if (!panel) return;
  const command = buildAgentInstallScriptCommand(window.location.origin);
  panel.innerHTML = `
    <div class="install-strip">
      <span class="install-label">添加节点</span>
      <code class="install-command">${escapeHTML(command)}</code>
      <button class="mini-action" type="button" data-copy-agent-install>复制</button>
    </div>
  `;
  panel.querySelector('[data-copy-agent-install]')?.addEventListener('click', async () => {
    if (await copyText(command)) {
      showToast('命令已复制');
      return;
    }
    showToast('复制失败，请手动复制', 'bad');
  });
}

async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to the legacy path for HTTP or denied clipboard access.
    }
  }
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.top = '-1000px';
  textarea.style.left = '-1000px';
  document.body.appendChild(textarea);
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);
  try {
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    document.body.removeChild(textarea);
  }
}

function renderNodeCard(node) {
  const discoveredApps = state.data.applications.filter((app) => appNodeName(app) === node.key);
  const managedApps = discoveredApps.filter(isManagedApp).length;
  const proxiedApps = state.data.applications.filter((app) => appProxyNode(app) === node.key).length;
  const pending = discoveredApps.filter(isAppIssue).length;
  const tone = isBadStatus(node.status) ? 'bad' : pending > 0 ? 'warning' : 'good';
  return `
    <article class="object-card is-clickable" role="link" tabindex="0" data-node-apps="${escapeAttribute(node.key)}" style="--card-accent: ${toneColor(tone)}">
      <div class="card-top">
        ${nodeAvatar(node)}
        <div class="card-title"><strong>${escapeHTML(node.title)}</strong><span>${escapeHTML(node.subtitle)}</span></div>
        <div class="card-side">
          <div class="card-badges">${statusIcon(node.status || 'success', tone)}</div>
          ${renderCardActions('节点操作', nodeMenuItems(node))}
        </div>
      </div>
      <div class="card-lines">
        ${dataLineHTML('应用', nodeApplicationRatio(managedApps, discoveredApps.length))}
        ${dataLine('代理出口', `${proxiedApps} 个`)}
        ${dataLineHTML('资源', nodeResourceLine(node.key))}
        ${node.kind === 'agent' || node.kind === 'pending-agent' ? dataLine('地址', node.address || '未设置') : ''}
      </div>
    </article>
  `;
}

function nodeResourceLine(nodeName) {
  const snapshot = state.data.resources.find((item) => item.node === nodeName);
  if (!snapshot || !snapshot.resources) {
    return '<span class="resource-summary is-muted">未采集</span>';
  }
  const resources = snapshot.resources;
  return `
    <span class="resource-summary" title="CPU ${formatPercent(resources.cpu_percent)} · 内存 ${formatPercent(resources.memory_percent)} · 磁盘 ${formatPercent(resources.disk_percent)}">
      ${resourceChip('CPU', resources.cpu_percent, 80, 90)}
      ${resourceChip('内存', resources.memory_percent, 80, 90)}
      ${resourceChip('磁盘', resources.disk_percent, 85, 95)}
    </span>
  `;
}

function resourceChip(label, value, warning, danger) {
  const number = Number(value);
  const tone = Number.isFinite(number) && number >= danger ? 'bad' : Number.isFinite(number) && number >= warning ? 'warning' : 'good';
  return `<span class="resource-chip" data-tone="${tone}">${escapeHTML(label)} ${formatPercent(value)}</span>`;
}

function formatPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '--';
  return `${Math.round(number)}%`;
}

function nodeApplicationRatio(managed, total) {
  if (total === 0) {
    return '<span class="ratio-empty">0 / 0</span>';
  }
  return `<span class="ratio-value"><strong>${escapeHTML(String(managed))}</strong><span>/ ${escapeHTML(String(total))}</span></span>`;
}

function nodeMenuItems(node) {
  if (node.kind === 'pending-agent') {
    return [
      { icon: 'check', label: '审批通过', attrs: `data-approve-agent="${escapeAttribute(node.name)}"` },
      { icon: 'delete', label: '忽略节点', attrs: `data-reject-agent="${escapeAttribute(node.name)}"`, danger: true },
    ];
  }
  if (node.kind === 'agent') {
    return [
      { icon: 'edit', label: '编辑节点', attrs: `data-edit-agent="${escapeAttribute(node.name)}"` },
      { icon: 'delete', label: '删除节点', attrs: `data-delete-agent="${escapeAttribute(node.name)}"`, danger: true },
    ];
  }
  if (node.kind === 'server') {
    return [{ icon: 'edit', label: '编辑控制节点', attrs: 'data-open-dialog="agent" data-dialog-id="server"' }];
  }
  return [];
}

function renderCardActions(label, items) {
  if (!items.length) return '';
  return `
    <div class="card-actions-inline" aria-label="${escapeAttribute(label)}">
      ${items.map((item) => `
        <button class="card-icon-action" type="button" aria-label="${escapeAttribute(item.label)}" title="${escapeAttribute(item.label)}" ${item.danger ? 'data-danger="true"' : ''} ${item.attrs}>
          ${iconSVG(item.icon)}
        </button>
      `).join('')}
    </div>
  `;
}

function iconSVG(name) {
  const paths = {
    edit: '<path d="m4 20 4.2-1 10-10a2.1 2.1 0 0 0-3-3l-10 10L4 20Z"/><path d="m13.5 6.5 3 3"/>',
    delete: '<path d="M4 7h16"/><path d="M10 11v6M14 11v6"/><path d="M6 7l1 13h10l1-13"/><path d="M9 7V4h6v3"/>',
    check: '<path d="m5 12 4 4L19 6"/>',
  };
  return `<svg viewBox="0 0 24 24" aria-hidden="true">${paths[name] || paths.edit}</svg>`;
}

function filteredApplications() {
  let apps = state.data.applications.slice();
  const type = appFilterType();
  if (type === 'node' && state.appFilter.node) {
    apps = apps.filter((app) => appBelongsToNode(app, state.appFilter.node));
  } else if (type === 'proxied') {
    apps = apps.filter(isManagedApp);
  } else if (type === 'unproxied') {
    apps = apps.filter(isAppIssue);
  } else if (type === 'discovered') {
    apps = apps.filter((app) => app.origin !== 'custom_app');
  } else if (type === 'custom') {
    apps = apps.filter((app) => app.origin === 'custom_app');
  }
  apps.sort((a, b) => Number(isManagedApp(a)) - Number(isManagedApp(b)) || String(a.name).localeCompare(String(b.name), 'zh-Hans-CN'));
  return apps;
}

function appFilterType() {
  return state.appFilter && state.appFilter.type ? state.appFilter.type : 'all';
}

function syncSelectedApp() {
  const apps = filteredApplications();
  if (!apps.find((app) => app.id === state.selectedAppID)) {
    state.selectedAppID = apps[0] ? apps[0].id : '';
  }
}

function isAppIssue(app) {
  return !isManagedApp(app);
}

function isManagedApp(app) {
  return ['proxied', 'success'].includes(String(app.status || '').toLowerCase());
}

function appSourceNode(app) {
  if (app.origin === 'custom_app') {
    return '自定义';
  }
  return app.source || (app.container && app.container.source) || 'server';
}

function appNodeName(app) {
  if (app.origin === 'custom_app') {
    return '';
  }
  return app.source || (app.container && app.container.source) || 'server';
}

function appProxyNode(app) {
  if (app.origin !== 'custom_app') {
    return '';
  }
  return app.exit_node || 'server';
}

function appBelongsToNode(app, nodeName) {
  return appNodeName(app) === nodeName || appProxyNode(app) === nodeName;
}

function appSourceLabel(app) {
  if (app.origin === 'custom_app') {
    return '自定义';
  }
  const source = app.source || (app.container && app.container.source) || 'server';
  if (app.origin === 'remote_agent') {
    return `${displayNodeName(source)} · 节点`;
  }
  const runtime = runtimeCopy[app.runtime || (app.container && app.container.runtime)] || 'Docker';
  return `${displayNodeName(source)} · ${runtime}`;
}

function appProxyLabel(app) {
  if (app.origin === 'custom_app') {
    return displayNodeName(app.exit_node || 'server');
  }
  return displayNodeName(appNodeName(app));
}

function displayNodeName(name) {
  if (name === 'server') return localNodeDisplayName();
  const agent = state.data.agents.find((item) => item.name === name) || state.data.pendingAgents.find((item) => item.name === name);
  return agentDisplayName(agent) || name;
}

function agentDisplayName(agent) {
  if (!agent) return '';
  return agent.display_name || agent.name || '';
}

function localNodeDisplayName() {
  const source = state.data.sources[0] || null;
  return source && source.display_name ? source.display_name : '控制节点';
}

function nodeItems() {
  const server = {
    kind: 'server',
    name: localNodeDisplayName(),
    title: localNodeDisplayName(),
    key: 'server',
    subtitle: nodeSubtitle('控制节点', ''),
    runtime: '',
    status: 'online',
    raw: {},
  };
  const localHealth = state.data.sources.map(sourceHealth).find((health) => isBadStatus(health.status)) || state.data.sources.map(sourceHealth).find((health) => health.status === 'pending');
  if (localHealth) {
    server.status = localHealth.status;
    server.raw = { last_error: localHealth.message };
  }
  const agents = state.data.agents.map((agent) => ({
    kind: 'agent',
    name: agent.name,
    title: agentDisplayName(agent),
    key: agent.name,
    subtitle: nodeSubtitle('远程节点', agent.runtime),
    runtime: agent.runtime,
    address: agent.addr,
    status: agent.status || (agent.last_error ? 'offline' : 'online'),
    raw: agent,
  }));
  const pending = state.data.pendingAgents.map((agent) => ({
    kind: 'pending-agent',
    name: agent.name,
    title: agentDisplayName(agent),
    key: agent.name,
    subtitle: nodeSubtitle('待审批节点', agent.runtime),
    runtime: agent.runtime,
    address: agent.addr,
    status: 'pending',
    raw: agent,
  }));
  return [server, ...pending, ...agents].sort((a, b) => (a.kind === 'server' ? -1 : b.kind === 'server' ? 1 : a.name.localeCompare(b.name, 'zh-Hans-CN')));
}

function nodeSubtitle(kind, runtime) {
  const runtimeText = runtimeCopy[runtime] || runtime || '';
  return runtimeText ? `${kind} · ${runtimeText}` : kind;
}

function certificateItems() {
  const byName = new Map(state.data.certificates.map((cert) => [cert.name, { ...cert, planned: false }]));
  for (const zone of state.data.zones) {
    if (!zone.certificate || !zone.certificate.enabled) {
      continue;
    }
    const plans = zone.certificate.bundles && zone.certificate.bundles.length > 0
      ? zone.certificate.bundles.map((bundle) => ({
        name: bundle.name ? `${zone.name}:${bundle.name}` : zone.name,
        zone: zone.name,
        domains: bundle.domains && bundle.domains.length ? bundle.domains : defaultCertificateDomains(zone),
        deploy_targets: bundle.deploy_targets && bundle.deploy_targets.length ? bundle.deploy_targets : zone.certificate.deploy_targets || [],
      }))
      : [{ name: zone.name, zone: zone.name, domains: defaultCertificateDomains(zone), deploy_targets: zone.certificate.deploy_targets || [] }];
    for (const plan of plans) {
      if (!byName.has(plan.name)) {
        byName.set(plan.name, { ...plan, planned: true });
      }
    }
  }
  return Array.from(byName.values()).sort((a, b) => String(a.name).localeCompare(String(b.name), 'zh-Hans-CN'));
}

function latestByTime(items, key) {
  return items.slice().sort((a, b) => new Date(b[key] || 0).getTime() - new Date(a[key] || 0).getTime())[0] || null;
}

function certificateStateTone(cert) {
  if (!cert.not_after) {
    return 'warning';
  }
  const expires = new Date(cert.not_after).getTime();
  if (!Number.isFinite(expires)) {
    return 'warning';
  }
  const now = Date.now();
  if (expires < now) {
    return 'bad';
  }
  if (expires - now < 14 * 24 * 60 * 60 * 1000) {
    return 'warning';
  }
  return 'good';
}

function ddnsSummary(latest, badState) {
  if (badState) {
    return ddnsErrorHint(badState);
  }
  if (!latest) {
    return '未同步';
  }
  return `成功 · ${formatDate(latest.synced_at)}`;
}

function ddnsProblemItems() {
  return state.data.ddns.filter((item) => isBadStatus(item.status) || item.error);
}

function ddnsErrorHint(item) {
  const provider = item.provider || 'DNS 服务商';
  const error = String(item.error || '').trim();
  if (!error) {
    return `${provider} 同步失败`;
  }
  if (/authorization header|api token|invalid token|unauthorized/i.test(error)) {
    return `${provider} 凭据无效，请检查配置`;
  }
  if (/permission denied|socket|docker daemon|connect/i.test(error)) {
    return `${provider} 无法连接容器，请检查节点`;
  }
  return `${provider} 同步失败`;
}

function certificateZoneSummary(certs, latestCert) {
  if (certs.length > 0) {
    return `${certs.length} 个计划`;
  }
  if (latestCert) {
    return '已签发';
  }
  return '等待签发';
}

function certificateSummary(zone, latestCert) {
  if (!zone.certificate || !zone.certificate.enabled) {
    return '未启用';
  }
  if (!latestCert || !latestCert.not_after) {
    return '等待签发';
  }
  const tone = certificateStateTone(latestCert);
  if (tone === 'bad') {
    return `失败 · 已过期 ${formatDate(latestCert.not_after)}`;
  }
  if (tone === 'warning') {
    return `将过期 · ${formatDate(latestCert.not_after)}`;
  }
  return `成功 · ${formatDate(latestCert.not_after)}`;
}

function latestDeployRun(cert) {
  const names = new Set([cert.name, ...(cert.deploy_targets || [])]);
  return state.data.deployments
    .filter((run) => run.bundle === cert.name || names.has(run.target))
    .sort((a, b) => new Date(b.finished_at || b.started_at || 0).getTime() - new Date(a.finished_at || a.started_at || 0).getTime())[0] || null;
}

function deployRunSummary(run) {
  const status = isBadStatus(run.status) ? '失败' : '成功';
  const when = formatDate(run.finished_at || run.started_at);
  return run.message ? `${status} · ${run.message}` : `${status} · ${when}`;
}

function latestDeployRunForZone(zone, certs) {
  const names = new Set(certs.map((cert) => cert.name));
  const targets = new Set();
  for (const cert of certs) {
    for (const target of cert.deploy_targets || []) {
      targets.add(target);
    }
  }
  for (const target of zone.certificate && zone.certificate.deploy_targets || []) {
    targets.add(target);
  }
  return state.data.deployments
    .filter((run) => names.has(run.bundle) || targets.has(run.target))
    .sort((a, b) => new Date(b.finished_at || b.started_at || 0).getTime() - new Date(a.finished_at || a.started_at || 0).getTime())[0] || null;
}

function deploySummary(zone, latestRun) {
  if (!zone.certificate || !zone.certificate.enabled) {
    return '未启用';
  }
  const targets = new Set(zone.certificate.deploy_targets || []);
  for (const bundle of zone.certificate.bundles || []) {
    for (const target of bundle.deploy_targets || []) {
      targets.add(target);
    }
  }
  if (targets.size === 0) {
    return '未配置';
  }
  if (!latestRun) {
    return `${targets.size} 个位置 · 待下发`;
  }
  return `${targets.size} 个位置 · ${deployRunSummary(latestRun)}`;
}

function sourceHealth(source) {
  const job = latestJobRun(source.runtime);
  if (!job) {
    return { status: 'online', message: '' };
  }
  if (isBadStatus(job.status)) {
    return { status: 'offline', message: runtimeErrorHint(job.message || '容器连接失败') };
  }
  if (job.status === 'running') {
    return { status: 'pending', message: job.message || '正在检测' };
  }
  return { status: 'online', message: '' };
}

function runtimeErrorHint(message) {
  const text = String(message || '').trim();
  if (text.startsWith('容器连接失败') || text.startsWith('无法连接容器')) {
    return text;
  }
  if (/permission denied|socket|docker daemon|connect/i.test(text)) {
    return '无法连接容器，请检查 Docker / Podman 权限';
  }
  if (!text) {
    return '无法连接容器';
  }
  return '无法连接容器';
}

function latestJobRun(name) {
  return state.data.jobs
    .filter((job) => job.name === name || job.name.endsWith(`.${name}`) || job.name.includes(name))
    .sort((a, b) => new Date(b.finished_at || b.started_at || 0).getTime() - new Date(a.finished_at || a.started_at || 0).getTime())[0] || null;
}

function defaultCertificateDomains(zone) {
  const domains = [zone.domain];
  if (zone.wildcard) {
    domains.push(`*.${zone.domain}`);
  }
  return domains.filter(Boolean);
}

function defaultZoneLabel() {
  const zone = state.data.zones.find((item) => item.default) || (state.data.zones.length === 1 ? state.data.zones[0] : null);
  return zone ? zone.name : '未设置';
}

function zoneFeatureStates(zone) {
  return [
    ['默认', Boolean(zone.default)],
    ['通配', Boolean(zone.wildcard)],
    ['DDNS', Boolean(zone.ddns && zone.ddns.enabled)],
    ['证书', Boolean(zone.certificate && zone.certificate.enabled)],
  ];
}

function zoneFeatureText(zone) {
  const enabled = zoneFeatureStates(zone).filter(([, active]) => active).map(([label]) => label);
  return enabled.length ? enabled.join(' · ') : '未启用自动能力';
}

function zoneFeatureLabel(zone) {
  const enabled = zoneFeatureStates(zone).filter(([, active]) => active).map(([label]) => label);
  if (!enabled.length) {
    return '<span class="domain-feature-text">未启用自动能力</span>';
  }
  return `<span class="domain-feature-text is-enabled">${escapeHTML(enabled.join(' · '))}</span>`;
}

async function handleAction(action, dataset = {}) {
  const url = actionURL(action, dataset);
  if (!url) {
    return;
  }
  try {
    const payload = await fetchJSON(url, { method: 'POST' });
    showToast(localizeError((payload && payload.message) || '操作已开始'));
    setTimeout(loadAll, 350);
  } catch (error) {
    showToast(error.message, 'bad');
  }
}

function actionURL(action, dataset) {
  const params = new URLSearchParams();
  if (dataset.zone) params.set('zone', dataset.zone);
  if (dataset.bundle) params.set('bundle', dataset.bundle);
  if (dataset.target) params.set('target', dataset.target);
  const query = params.toString();
  if (action === 'routes-refresh') return actionEndpoints.routesRefresh;
  if (action === 'ddns-sync') return actionEndpoints.ddnsSync + (query ? `?${query}` : '');
  if (action === 'cert-renew') return actionEndpoints.certRenew + (query ? `?${query}` : '');
  if (action === 'cert-deploy') return actionEndpoints.certDeploy + (query ? `?${query}` : '');
  return '';
}

function openDialog(kind, id = '') {
  state.editing[kind] = id || '';
  if (kind === 'custom-app') fillCustomAppForm(id);
  if (kind === 'zone') fillZoneForm(id);
  if (kind === 'provider') showProviderManager();
  if (kind === 'provider-edit') fillProviderForm(id);
  if (kind === 'agent') fillAgentForm(id);
  if (kind === 'deploy-target') showDeployTargetManager();
  if (kind === 'deploy-target-edit') fillDeployTargetForm(id);
  document.getElementById(`${kind}-dialog`).showModal();
}

function closeDialog(kind) {
  document.getElementById(`${kind}-dialog`)?.close();
}

function fillCustomAppForm(name) {
  populateSelects();
  const app = state.data.customApps.find((item) => item.name === name) || null;
  setValue('custom-app-mode', app ? 'edit' : 'create');
  setText('custom-app-dialog-title', app ? '编辑应用' : '添加应用');
  setValue('custom-app-name', app ? app.name : '');
  setValue('custom-app-icon', app ? app.icon || '' : '');
  setValue('custom-app-zone', app ? app.zone : defaultZoneValue());
  setValue('custom-app-subdomain', app ? app.subdomain || '' : '');
  setValue('custom-app-exit-node', app ? app.exit_node || '' : '');
  setValue('custom-app-target-url', app ? app.target_url || '' : '');
  document.getElementById('custom-app-name').disabled = false;
}

async function submitCustomApp(event) {
  event.preventDefault();
  const mode = value('custom-app-mode');
  const name = value('custom-app-name');
  const originalName = state.editing['custom-app'] || name;
  const zoneName = value('custom-app-zone');
  if (!value('custom-app-subdomain')) {
    showToast('自定义应用必须填写子域名', 'bad');
    document.getElementById('custom-app-subdomain').focus();
    return;
  }
  const payload = {
    name,
    icon: value('custom-app-icon'),
    zone: value('custom-app-zone'),
    subdomain: value('custom-app-subdomain'),
    exit_node: value('custom-app-exit-node'),
    target_url: value('custom-app-target-url'),
  };
  await saveResource(mode === 'edit' ? `/api/v1/apps/${encodeURIComponent(originalName)}` : endpoints.customApps, mode === 'edit' ? 'PUT' : 'POST', payload, 'custom-app');
}

async function deleteCustomAppByName(name) {
  if (!name || !confirm(`删除手动添加应用 ${name}？`)) return;
  await deleteResource(`/api/v1/apps/${encodeURIComponent(name)}`, 'custom-app');
}

function fillZoneForm(name) {
  populateSelects();
  const zone = state.data.zones.find((item) => item.name === name) || null;
  setValue('zone-mode', zone ? 'edit' : 'create');
  setText('zone-dialog-title', zone ? '编辑域名' : '添加域名');
  setValue('zone-name', zone ? zone.name : '');
  setValue('zone-domain', zone ? zone.domain : '');
  setChecked('zone-default', Boolean(zone && zone.default));
  setChecked('zone-wildcard', zone ? Boolean(zone.wildcard) : true);
  setChecked('zone-ddns-enabled', Boolean(zone && zone.ddns && zone.ddns.enabled));
  setChecked('zone-ddns-ipv6', Boolean(zone && zone.ddns && zone.ddns.ipv6));
  setValue('zone-ddns-provider', zone ? zoneProviderRef(zone) : '');
  renderCertificateDeployTargetChoices(zone && zone.certificate ? zone.certificate.deploy_targets || [] : []);
  updateZoneProviderDependentFields();
}

async function submitZone(event) {
  event.preventDefault();
  const mode = value('zone-mode');
  const domain = value('zone-domain');
  const name = mode === 'edit' ? value('zone-name') : zoneNameFromDomain(domain);
  const existing = state.data.zones.find((item) => item.name === name);
  const providerRef = value('zone-ddns-provider');
  const certificateEnabled = Boolean(providerRef);
  if (checked('zone-ddns-enabled') && !providerRef) {
    showToast('请选择 DNS 服务商', 'bad');
    document.getElementById('zone-ddns-provider').focus();
    return;
  }
  const deployTargets = certificateChoiceValues('zone-certificate-deploy-targets');
  const payload = {
    name,
    domain,
    default: checked('zone-default'),
    wildcard: checked('zone-wildcard'),
    ddns: {
      enabled: checked('zone-ddns-enabled'),
      provider_refs: checked('zone-ddns-enabled') && providerRef ? [providerRef] : [],
      ipv4: true,
      ipv6: checked('zone-ddns-ipv6'),
      wildcard: checked('zone-wildcard'),
      ttl: 300,
    },
    certificate: {
      ...(existing && existing.certificate ? existing.certificate : { enabled: false, email: '', dns_provider: '', renew_before: 0, deploy_targets: [], bundles: [] }),
      enabled: certificateEnabled,
      email: defaultCertificateEmail(domain, existing && existing.certificate ? existing.certificate.email : ''),
      dns_provider: providerRef,
      renew_before: 20 * 24 * 3600 * 1000000000,
      deploy_targets: deployTargets,
    },
  };
  await saveResource(mode === 'edit' ? `/api/v1/zones/${encodeURIComponent(name)}` : endpoints.zones, mode === 'edit' ? 'PUT' : 'POST', payload, 'zone');
}

async function deleteZoneByName(name) {
  if (!name || !confirm(`删除域名 ${name}？`)) return;
  await deleteResource(`/api/v1/zones/${encodeURIComponent(name)}`, '');
}

function fillProviderForm(ref) {
  const provider = state.data.providers.find((item) => item.ref === ref) || null;
  setValue('provider-mode', provider ? 'edit' : 'create');
  setText('provider-edit-title', provider ? '编辑 DNS 服务商' : '新增 DNS 服务商');
  setValue('provider-ref', provider ? provider.ref : '');
  if (provider && provider.type) ensureOption('provider-type', provider.type, provider.type);
  setValue('provider-type', provider ? provider.type : '');
  setValue('provider-options', provider ? stringifyOptions(provider.options || {}) : '');
  const providerRefInput = document.getElementById('provider-ref');
  providerRefInput.disabled = Boolean(provider);
  providerRefInput.readOnly = Boolean(provider);
}

function showProviderManager() {
  renderProviderManagerList();
}

function renderProviderManagerList() {
  const list = document.getElementById('provider-manager-list');
  if (!list) return;
  const providers = state.data.providers;
  list.innerHTML = `
    <div class="manager-list-head"><span>已配置 ${providers.length} 个 DNS</span><button class="mini-action" type="button" data-provider-create>新建</button></div>
    <table class="manager-table">
      <thead><tr><th>标识</th><th>类型</th><th>配置</th><th></th></tr></thead>
      <tbody>
        ${providers.length ? providers.map((provider) => `
          <tr>
            <td>${escapeHTML(provider.ref)}</td>
            <td>${escapeHTML(provider.type || '未知')}</td>
            <td>${Object.keys(provider.options || {}).length} 项</td>
            <td class="table-actions"><button class="mini-action" type="button" data-manage-provider="${escapeAttribute(provider.ref)}">编辑</button><button class="mini-action" type="button" data-remove-provider="${escapeAttribute(provider.ref)}">删除</button></td>
          </tr>
        `).join('') : '<tr><td colspan="4">还没有 DNS 服务商</td></tr>'}
      </tbody>
    </table>
  `;
  const create = list.querySelector('[data-provider-create]');
  if (create) create.addEventListener('click', () => openProviderEditor(''));
  for (const row of list.querySelectorAll('[data-manage-provider]')) {
    row.addEventListener('click', () => openProviderEditor(row.dataset.manageProvider));
  }
  for (const remove of list.querySelectorAll('[data-remove-provider]')) {
    remove.addEventListener('click', () => deleteProviderByRef(remove.dataset.removeProvider));
  }
}

function openProviderEditor(ref) {
  fillProviderForm(ref);
  closeDialog('provider');
  document.getElementById('provider-edit-dialog').showModal();
}

async function submitProvider(event) {
  event.preventDefault();
  const mode = value('provider-mode');
  const ref = value('provider-ref');
  const payload = { ref, type: value('provider-type'), options: parseOptions(value('provider-options')) };
  await saveResource(mode === 'edit' ? `/api/v1/ddns-providers/${encodeURIComponent(ref)}` : endpoints.providers, mode === 'edit' ? 'PUT' : 'POST', payload, 'provider-edit');
}

async function deleteProviderByRef(ref) {
  if (!ref || !confirm(`删除 DNS 服务商 ${ref}？`)) return;
  await deleteResource(`/api/v1/ddns-providers/${encodeURIComponent(ref)}`, 'provider');
}

function fillLocalNodeForm(runtime) {
  if (!runtime && state.data.sources.length > 0) {
    runtime = state.data.sources[0].runtime || '';
  }
  const source = state.data.sources.find((item) => item.runtime === runtime) || null;
  setValue('agent-mode', source ? 'edit' : 'create');
  setValue('agent-kind', 'source');
  setText('agent-dialog-title', '编辑控制节点');
  setValue('agent-name', 'server');
  setValue('agent-runtime-original', source ? source.runtime || '' : runtime || '');
  setValue('agent-display-name', source ? source.display_name || '控制节点' : '控制节点');
  setValue('agent-addr', source ? source.endpoint || '' : 'unix:///var/run/docker.sock');
  setValue('agent-runtime', source ? source.runtime || 'docker' : 'docker');
  setValue('agent-socket-path', '');
  document.getElementById('agent-runtime').disabled = false;
  document.querySelector('[data-agent-field="socket"]').hidden = true;
  document.querySelector('[data-agent-field="network"]').hidden = false;
  document.getElementById('agent-save').hidden = false;
  void updateAgentNetworkOptions(source ? source.network || '' : '');
}

async function deleteSourceByName(runtime) {
  if (!runtime || !confirm(`删除节点运行时 ${runtimeCopy[runtime] || runtime}？`)) return;
  await deleteResource(`/api/v1/runtimes/${encodeURIComponent(runtime)}`, '');
}

async function updateAgentNetworkOptions(selected = '') {
  if (value('agent-kind') !== 'source') return;
  const select = document.getElementById('agent-network');
  const current = typeof selected === 'string' ? selected : select.value;
  const runtime = value('agent-runtime') || 'docker';
  const endpoint = value('agent-addr');
  setOptions('agent-network', [['', '默认网络']]);
  try {
    const params = new URLSearchParams({ runtime });
    if (endpoint) params.set('endpoint', endpoint);
    const networks = await fetchJSON(`/api/v1/runtimes/networks?${params.toString()}`);
    const options = [['', '默认网络'], ...(Array.isArray(networks) ? networks.map((name) => [name, name]) : [])];
    if (current && !options.find(([valueText]) => valueText === current)) {
      options.push([current, current]);
    }
    setOptions('agent-network', options);
    setValue('agent-network', current);
  } catch {
    if (current) {
      setOptions('agent-network', [['', '默认网络'], [current, current]]);
      setValue('agent-network', current);
    }
  }
}

function fillAgentForm(name) {
  if (name === 'server') {
    fillLocalNodeForm('');
    return;
  }
  const agent = state.data.agents.find((item) => item.name === name) || null;
  if (!agent) return;
  setValue('agent-mode', agent ? 'edit' : 'create');
  setValue('agent-kind', 'agent');
  setValue('agent-runtime-original', '');
  setText('agent-dialog-title', '编辑节点');
  setValue('agent-name', agent ? agent.name : '');
  setValue('agent-display-name', agent ? agentDisplayName(agent) : '');
  setValue('agent-addr', agent ? agent.addr || '' : '');
  setValue('agent-runtime', agent ? agent.runtime || 'docker' : 'docker');
  setValue('agent-socket-path', agent ? agent.socket_path || '' : '');
  setOptions('agent-network', [['', '默认网络']]);
  document.getElementById('agent-runtime').disabled = false;
  document.querySelector('[data-agent-field="socket"]').hidden = false;
  document.querySelector('[data-agent-field="network"]').hidden = true;
  document.getElementById('agent-save').hidden = false;
}

async function submitAgent(event) {
  event.preventDefault();
  const mode = value('agent-mode');
  const kind = value('agent-kind');
  const name = value('agent-name');
  if (kind === 'source') {
    const runtime = value('agent-runtime');
    const originalRuntime = value('agent-runtime-original') || runtime;
    const payload = {
      display_name: value('agent-display-name'),
      runtime,
      endpoint: value('agent-addr'),
      network: value('agent-network'),
    };
    await saveResource(mode === 'edit' ? `/api/v1/runtimes/${encodeURIComponent(originalRuntime)}` : endpoints.sources, mode === 'edit' ? 'PUT' : 'POST', payload, 'agent');
    return;
  }
  const payload = {
    name,
    display_name: value('agent-display-name'),
    addr: value('agent-addr'),
    runtime: value('agent-runtime'),
    socket_path: value('agent-socket-path'),
  };
  await saveResource(mode === 'edit' ? `/api/v1/agents/${encodeURIComponent(name)}` : endpoints.agents, mode === 'edit' ? 'PUT' : 'POST', payload, 'agent');
}

async function deleteAgentByName(name) {
  if (!name || !confirm(`删除节点 ${name}？`)) return;
  await deleteResource(`/api/v1/agents/${encodeURIComponent(name)}`, '');
}

async function approvePendingAgent(name) {
  if (!name) return;
  await saveResource(`/api/v1/agents/pending/${encodeURIComponent(name)}/approve`, 'POST', {}, '');
}

async function rejectPendingAgent(name) {
  if (!name) return;
  await deleteResource(`/api/v1/agents/pending/${encodeURIComponent(name)}`, '');
}

function buildAgentInstallScriptCommand(serverURL) {
  return `curl -fsSL ${serverURL}/api/v1/agents/install.sh | sudo sh`;
}

function shellQuote(text) {
  return `'${String(text || '').replace(/'/g, `'\\''`)}'`;
}

function fillDeployTargetForm(name) {
  populateSelects();
  const target = state.data.targets.find((item) => item.name === name) || null;
  setValue('deploy-target-mode', target ? 'edit' : 'create');
  setText('deploy-target-edit-title', target ? '编辑部署位置' : '新增部署位置');
  setValue('deploy-target-name', target ? target.name : '');
  setValue('deploy-target-transport', target ? target.transport || 'local' : 'local');
  setValue('deploy-target-agent', target && target.agent ? target.agent.node || '' : '');
  const sshEndpoint = normalizeSSHEndpoint(target && target.ssh ? target.ssh.addr || '' : '', target && target.ssh ? target.ssh.port || 0 : 0);
  setValue('deploy-target-ssh-addr', formatSSHEndpoint(sshEndpoint));
  setValue('deploy-target-ssh-user', target && target.ssh ? target.ssh.user || '' : '');
  setValue('deploy-target-ssh-key', target && target.ssh ? target.ssh.private_key_path || '' : '');
  setValue('deploy-target-cert-path', target ? target.cert_path || '' : '');
  setValue('deploy-target-key-path', target ? target.key_path || '' : '');
  setValue('deploy-target-reload', target ? target.reload_command || '' : '');
  document.getElementById('deploy-target-name').disabled = Boolean(target);
  updateDeployTargetTransportFields();
}

function showDeployTargetManager() {
  renderDeployTargetManagerList();
}

function renderDeployTargetManagerList() {
  const list = document.getElementById('deploy-target-manager-list');
  if (!list) return;
  const targets = state.data.targets;
  list.innerHTML = `
    <div class="manager-list-head"><span>已配置 ${targets.length} 个部署位置</span><button class="mini-action" type="button" data-target-create>新建</button></div>
    <table class="manager-table">
      <thead><tr><th>名称</th><th>方式</th><th>证书路径</th><th></th></tr></thead>
      <tbody>
        ${targets.length ? targets.map((target) => `
          <tr>
            <td>${escapeHTML(target.name)}</td>
            <td>${escapeHTML(transportCopy[target.transport] || target.transport)}</td>
            <td>${escapeHTML(target.cert_path || '未设置路径')}</td>
            <td class="table-actions"><button class="mini-action" type="button" data-manage-target="${escapeAttribute(target.name)}">编辑</button><button class="mini-action" type="button" data-remove-target="${escapeAttribute(target.name)}">删除</button></td>
          </tr>
        `).join('') : '<tr><td colspan="4">还没有部署位置</td></tr>'}
      </tbody>
    </table>
  `;
  const create = list.querySelector('[data-target-create]');
  if (create) create.addEventListener('click', () => openDeployTargetEditor(''));
  for (const row of list.querySelectorAll('[data-manage-target]')) {
    row.addEventListener('click', () => openDeployTargetEditor(row.dataset.manageTarget));
  }
  for (const remove of list.querySelectorAll('[data-remove-target]')) {
    remove.addEventListener('click', () => deleteDeployTargetByName(remove.dataset.removeTarget));
  }
}

function openDeployTargetEditor(name) {
  fillDeployTargetForm(name);
  closeDialog('deploy-target');
  document.getElementById('deploy-target-edit-dialog').showModal();
}

async function submitDeployTarget(event) {
  event.preventDefault();
  const mode = value('deploy-target-mode');
  const name = value('deploy-target-name');
  const sshEndpoint = normalizeSSHEndpoint(value('deploy-target-ssh-addr'), 0);
  const payload = {
    name,
    transport: value('deploy-target-transport'),
    local: {},
    agent: { node: value('deploy-target-agent') },
    ssh: {
      addr: sshEndpoint.host,
      user: value('deploy-target-ssh-user'),
      port: sshEndpoint.port,
      private_key_path: value('deploy-target-ssh-key'),
    },
    cert_path: value('deploy-target-cert-path'),
    key_path: value('deploy-target-key-path'),
    reload_command: value('deploy-target-reload'),
  };
  await saveResource(mode === 'edit' ? `/api/v1/deploy-targets/${encodeURIComponent(name)}` : endpoints.targets, mode === 'edit' ? 'PUT' : 'POST', payload, 'deploy-target-edit');
}

function normalizeSSHEndpoint(rawHost, rawPort) {
  let host = String(rawHost || '').trim();
  let port = Number.parseInt(String(rawPort || '0'), 10) || 0;
  const bracketed = host.match(/^\[([^\]]+)]:(\d+)$/);
  if (bracketed) {
    return { host: bracketed[1], port: port || Number.parseInt(bracketed[2], 10) || 0 };
  }
  const hostPort = host.match(/^([^:]+):(\d+)$/);
  if (hostPort) {
    host = hostPort[1];
    port = port || Number.parseInt(hostPort[2], 10) || 0;
  }
  return { host, port };
}

function formatSSHEndpoint(endpoint) {
  const host = endpoint.host || '';
  if (!host || !endpoint.port) return host;
  return host.includes(':') ? `[${host}]:${endpoint.port}` : `${host}:${endpoint.port}`;
}

async function deleteDeployTargetByName(name) {
  if (!name || !confirm(`删除下发目标 ${name}？`)) return;
  await deleteResource(`/api/v1/deploy-targets/${encodeURIComponent(name)}`, '');
}

function updateDeployTargetTransportFields() {
  const transport = value('deploy-target-transport');
  for (const field of document.querySelectorAll('[data-transport-section]')) {
    const visible = field.dataset.transportSection === transport || (transport === 'local' && field.dataset.transportSection === 'local');
    field.hidden = !visible;
  }
}

async function saveResource(url, method, payload, dialogKind) {
  try {
    await fetchJSON(url, { method, body: JSON.stringify(payload) });
    closeDialog(dialogKind);
    showToast('已保存');
    await loadAll();
  } catch (error) {
    showToast(error.message, 'bad');
  }
}

async function deleteResource(url, dialogKind) {
  try {
    await fetchJSON(url, { method: 'DELETE' });
    if (dialogKind) closeDialog(dialogKind);
    showToast('已删除');
    await loadAll();
  } catch (error) {
    showToast(error.message, 'bad');
  }
}

function populateSelects() {
  setOptions('custom-app-zone', state.data.zones.map((zone) => [zone.name, zone.domain]));
  setOptions('custom-app-exit-node', [['', localNodeDisplayName()], ...state.data.agents.map((agent) => [agent.name, agentDisplayName(agent)])]);
  setOptions('zone-ddns-provider', state.data.providers.map((provider) => [provider.ref, `${provider.ref} · ${provider.type}`]), '未选择');
  setOptions('deploy-target-agent', state.data.agents.map((agent) => [agent.name, agentDisplayName(agent)]), '选择节点');
  setOptions('provider-type', providerCatalogTypes().map((type) => [type, type]));
}

function defaultProviderValue() {
  return state.data.providers[0] ? state.data.providers[0].ref : '';
}

function zoneNameFromDomain(domain) {
  const cleaned = String(domain || '').trim().toLowerCase();
  const first = cleaned.split('.')[0];
  return first || cleaned.replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '') || 'domain';
}

function renderCertificateDeployTargetChoices(selectedValues = []) {
  const container = document.getElementById('zone-certificate-deploy-targets');
  const selected = new Set(selectedValues || []);
  if (!state.data.targets.length) {
    container.innerHTML = '<div class="multi-picker-empty">还没有部署位置，请先在域名页顶部新建。</div>';
    updateDeployTargetSummary();
    return;
  }
  container.innerHTML = state.data.targets.map((target) => `
    <label class="multi-picker-option">
      <input type="checkbox" value="${escapeAttribute(target.name)}" ${selected.has(target.name) ? 'checked' : ''}>
      <span><strong>${escapeHTML(target.name)}</strong><small>${escapeHTML(transportCopy[target.transport] || target.transport)} · ${escapeHTML(target.cert_path || '未设置路径')}</small></span>
    </label>
  `).join('');
  updateDeployTargetSummary();
}

function certificateChoiceValues(id) {
  const element = document.getElementById(id);
  return Array.from(document.querySelectorAll(`#${id} input:checked`)).map((input) => input.value).filter(Boolean);
}

function updateDeployTargetSummary() {
  const summary = document.getElementById('zone-certificate-deploy-targets-summary');
  const pickerSummary = document.querySelector('.multi-picker summary');
  if (!summary) return;
  if (!state.data.targets.length) {
    summary.textContent = '无可用位置';
    if (pickerSummary) pickerSummary.textContent = '请先新建部署位置';
    return;
  }
  const values = certificateChoiceValues('zone-certificate-deploy-targets');
  summary.textContent = values.length ? `已选择 ${values.length} 个` : '未选择';
  if (pickerSummary) pickerSummary.textContent = values.length ? values.join('、') : '无';
}

function updateZoneProviderDependentFields() {
  const hasProvider = Boolean(value('zone-ddns-provider'));
  for (const field of document.querySelectorAll('[data-provider-dependent]')) {
    field.hidden = !hasProvider;
  }
  if (!hasProvider) {
    setChecked('zone-ddns-enabled', false);
    setChecked('zone-ddns-ipv6', false);
  }
}

function providerCatalogTypes() {
  const types = new Set();
  for (const entry of state.data.providerCatalog || []) {
    const value = entry.type || entry.name || entry.id || entry.provider;
    if (value) types.add(String(value));
  }
  return Array.from(types).sort();
}

function setOptions(id, options, placeholder = '') {
  const select = document.getElementById(id);
  const current = select.multiple ? multiValue(id) : select.value;
  select.innerHTML = `${placeholder ? `<option value="">${escapeHTML(placeholder)}</option>` : ''}${options.map(([valueText, label]) => `<option value="${escapeAttribute(valueText)}">${escapeHTML(label)}</option>`).join('')}`;
  if (select.multiple && Array.isArray(current)) {
    setMultiValue(id, current);
  } else if (!select.multiple && current) {
    select.value = current;
  }
}

function ensureOption(id, valueText, label = valueText) {
  const select = document.getElementById(id);
  if (!select || !valueText || Array.from(select.options).some((option) => option.value === valueText)) return;
  select.appendChild(new Option(label, valueText));
}

function zoneProviderRef(zone) {
  if (!zone) return '';
  if (zone.ddns && zone.ddns.provider_refs && zone.ddns.provider_refs[0]) return zone.ddns.provider_refs[0];
  return '';
}

function defaultCertificateEmail(domain, existingEmail = '') {
  const current = String(existingEmail || '').trim();
  return current || `admin@${String(domain || 'example.com').trim() || 'example.com'}`;
}

function setMultiValue(id, values) {
  const set = new Set(values || []);
  for (const option of document.getElementById(id).options) {
    option.selected = set.has(option.value);
  }
}

function multiValue(id) {
  return Array.from(document.getElementById(id).selectedOptions).map((option) => option.value).filter(Boolean);
}

function defaultZoneValue() {
  const zone = state.data.zones.find((item) => item.default) || state.data.zones[0];
  return zone ? zone.name : '';
}

function parseOptions(text) {
  const options = {};
  for (const rawLine of text.split('\n')) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const separator = line.includes('=') ? '=' : ':';
    const index = line.indexOf(separator);
    if (index < 0) continue;
    const key = line.slice(0, index).trim();
    const optionValue = line.slice(index + 1).trim();
    if (key) options[key] = optionValue;
  }
  return options;
}

function stringifyOptions(options) {
  return Object.entries(options || {}).map(([key, optionValue]) => `${key}=${optionValue}`).join('\n');
}

function value(id) {
  return document.getElementById(id).value.trim();
}

function setValue(id, nextValue) {
  document.getElementById(id).value = nextValue == null ? '' : String(nextValue);
}

function checked(id) {
  return document.getElementById(id).checked;
}

function setChecked(id, nextValue) {
  document.getElementById(id).checked = Boolean(nextValue);
}

function setText(id, nextValue) {
  document.getElementById(id).textContent = nextValue;
}

function avatar(name, icon) {
  if (isImageIcon(icon)) {
    return `<span class="avatar avatar-image"><img src="${escapeAttribute(icon)}" alt=""></span>`;
  }
  const label = icon || initials(name);
  return `<span class="avatar" aria-hidden="true">${escapeHTML(label)}</span>`;
}

function isImageIcon(icon) {
  const valueText = String(icon || '').trim();
  return /^(https?:\/\/|\/|\.\/|\.\.\/|data:image\/)/i.test(valueText);
}

function fixedAvatar(kind) {
  const labels = { domain: '域', node: '节' };
  return `<span class="avatar avatar-fixed" aria-hidden="true">${escapeHTML(labels[kind] || 'D')}</span>`;
}

function countAvatar(count, label) {
  return `<span class="avatar avatar-count" aria-label="${escapeAttribute(label)} ${escapeAttribute(String(count))}"><strong>${escapeHTML(String(count))}</strong></span>`;
}

function nodeAvatar(node) {
  if (node && node.kind === 'server') {
    return '<span class="avatar avatar-logo"><img src="/logo.svg" alt=""></span>';
  }
  return fixedAvatar('node');
}

function initials(name) {
  const cleaned = String(name || 'D').trim();
  if (!cleaned) return 'D';
  const words = cleaned.split(/[\s._-]+/).filter(Boolean);
  if (words.length >= 2) return `${words[0][0]}${words[1][0]}`.toUpperCase();
  return cleaned.slice(0, 3).toUpperCase();
}

function statusBadge(status, tone = statusTone(status)) {
  return `<span class="status-badge" data-tone="${escapeAttribute(tone)}">${escapeHTML(statusCopy[status] || status || '状态未知')}</span>`;
}

function statusIcon(status, tone = statusTone(status)) {
  const label = statusCopy[status] || status || '状态未知';
  return `<span class="status-icon" data-tone="${escapeAttribute(tone)}" title="连接状态：${escapeAttribute(label)}" aria-label="连接状态：${escapeAttribute(label)}" role="img"></span>`;
}

function dataLine(label, content, hint = '') {
  const title = hint || content || '';
  return `<div class="data-line"><span>${escapeHTML(label)}</span><span title="${escapeAttribute(title)}">${escapeHTML(content || '无')}</span></div>`;
}

function dataLineHTML(label, html, hint = '') {
  return `<div class="data-line"><span>${escapeHTML(label)}</span><span title="${escapeAttribute(hint || '')}">${html || '无'}</span></div>`;
}

function clickableDataLine(label, content, hint, zoneName) {
  return `<button type="button" class="data-line data-line-action" data-sync-zone="${escapeAttribute(zoneName)}" title="${escapeAttribute(hint || '')}"><span>${escapeHTML(label)}</span><span title="${escapeAttribute(hint || content || '')}">${escapeHTML(content || '无')}</span></button>`;
}

function detailLine(label, content) {
  return `<div class="detail-line"><span>${escapeHTML(label)}</span><span title="${escapeAttribute(content || '')}">${escapeHTML(content || '无')}</span></div>`;
}

function emptyState(title, message) {
  return `<div class="empty-state"><div><strong>${escapeHTML(title)}</strong><p>${escapeHTML(message)}</p></div></div>`;
}

function statusTone(status) {
  if (isBadStatus(status)) return 'bad';
  if (['unproxied', 'pending'].includes(status)) return 'warning';
  if (['success', 'proxied', 'online', 'noop'].includes(status)) return 'good';
  return 'info';
}

function isBadStatus(status) {
  return ['failed', 'error', 'offline', 'failure'].includes(String(status || '').toLowerCase());
}

function toneColor(tone) {
  if (tone === 'bad') return 'var(--danger)';
  if (tone === 'warning') return 'var(--warning)';
  if (tone === 'info') return 'var(--info)';
  return 'var(--accent)';
}

function formatDate(valueText) {
  if (!valueText) return '无';
  const date = new Date(valueText);
  if (!Number.isFinite(date.getTime()) || date.getTime() <= 0) return '无';
  return date.toLocaleString('zh-CN', { hour12: false });
}

function localizeError(message) {
  const text = String(message || '').trim();
  const lower = text.toLowerCase();
  const map = {
    'routes refresh started': '已开始刷新应用入口。',
    'ddns sync started': '已开始同步域名解析。',
    'certificate renew started': '已开始检查证书续期。',
    'certificate deploy started': '已开始下发证书。',
    'custom app deleted': '应用已删除。',
    'zone deleted': '域名已删除。',
    'ddns provider deleted': 'DNS 连接已删除。',
    'docker source deleted': '节点运行时已删除。',
    'runtime deleted': '节点运行时已删除。',
    'agent deleted': '节点已删除。',
    'deploy target deleted': '下发目标已删除。',
    'configuration manager is not configured': '当前进程未启用控制台写配置能力。',
  };
  return map[lower] || text || '操作失败';
}

function showToast(message, tone = 'good') {
  const toast = document.getElementById('toast');
  toast.textContent = message;
  toast.dataset.tone = tone;
  toast.hidden = false;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => {
    toast.hidden = true;
  }, 4600);
}

function restoreTheme() {
  const saved = localStorage.getItem('domux-theme');
  const theme = saved || (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
  document.documentElement.dataset.theme = theme;
}

function toggleTheme() {
  const current = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
  const next = current === 'light' ? 'dark' : 'light';
  document.documentElement.dataset.theme = next;
  localStorage.setItem('domux-theme', next);
}

function escapeHTML(valueText) {
  return String(valueText == null ? '' : valueText)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function escapeAttribute(valueText) {
  return escapeHTML(valueText);
}
