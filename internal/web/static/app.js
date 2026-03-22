/* Ops Monitor — Vue 3 + Naive UI SPA */
const { createApp, ref, reactive, computed, onMounted, onUnmounted, h, watch, nextTick, defineComponent, provide, inject } = Vue;
const { createRouter, createWebHashHistory } = VueRouter;
const {
    NConfigProvider, NLayout, NLayoutSider, NMenu, NButton, NIcon, NSpace,
    NCard, NStatistic, NGrid, NGi, NDataTable, NModal, NForm, NFormItem,
    NInput, NInputNumber, NSelect, NSwitch, NPopconfirm, NTag, NAvatar,
    NResult, NSpin, NBadge, NAlert, NEmpty, NText, NDivider, NScrollbar,
    NInputGroup, NTooltip, NMessageProvider, useMessage, darkTheme,
    NDropdown, NPageHeader, NPagination, NDescriptions, NDescriptionsItem,
    NDrawer, NDrawerContent
} = naive;

// ============================================================
// Session cache
// ============================================================
let _sessionValid = false;

// ============================================================
// Global responsive state
// ============================================================
const _isMobile = ref(window.innerWidth < 768);
window.addEventListener('resize', () => { _isMobile.value = window.innerWidth < 768; });

// ============================================================
// Theme state (default light)
// ============================================================
const _isDark = ref(localStorage.getItem('theme') === 'dark');
watch(_isDark, v => {
    localStorage.setItem('theme', v ? 'dark' : 'light');
    document.documentElement.setAttribute('data-theme', v ? 'dark' : 'light');
});
document.documentElement.setAttribute('data-theme', _isDark.value ? 'dark' : 'light');

function toggleTheme() { _isDark.value = !_isDark.value; }
function themeIcon() { return _isDark.value ? '\u2600' : '\u263e'; }

// ============================================================
// API Helper
// ============================================================
const api = {
    async request(method, url, body) {
        const opts = { method, headers: {} };
        if (body) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        let res;
        try {
            res = await fetch(url, opts);
        } catch (e) {
            // Network error (server restart, offline) — don't logout
            throw new Error('network_error');
        }
        if (res.status === 401) {
            _sessionValid = false;
            window.location.hash = '#/login';
            throw new Error('unauthorized');
        }
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || 'request failed');
        return data;
    },
    get(url) { return this.request('GET', url); },
    post(url, body) { return this.request('POST', url, body); },
    put(url, body) { return this.request('PUT', url, body); },
    del(url) { return this.request('DELETE', url); },
};

// ============================================================
// WebSocket composable
// ============================================================
function useWebSocket(path) {
    const connected = ref(false);
    const messages = ref([]);
    let ws = null;
    let retryDelay = 1000;
    let stopped = false;

    function connect() {
        if (stopped) return;
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(proto + '//' + location.host + path);
        ws.onopen = () => { connected.value = true; retryDelay = 1000; };
        ws.onclose = () => {
            connected.value = false;
            if (!stopped) setTimeout(connect, retryDelay);
            retryDelay = Math.min(retryDelay * 2, 30000);
        };
        ws.onmessage = (e) => {
            try { messages.value.push(JSON.parse(e.data)); } catch {}
        };
    }

    function stop() { stopped = true; if (ws) ws.close(); }
    function clear() { messages.value = []; }

    connect();
    return { connected, messages, stop, clear };
}

// ============================================================
// Helper: responsive columns
// ============================================================
function useColumns(allColumns) {
    return computed(() => {
        if (_isMobile.value) return allColumns.filter(c => !c._hideOnMobile);
        return allColumns;
    });
}

// ============================================================
// Global SQL detail modal
// ============================================================
const _sqlDetail = reactive({ show: false, sql: '', row: null });
function showSqlDetail(row) {
    _sqlDetail.sql = row.sql_text || '';
    _sqlDetail.row = row;
    _sqlDetail.show = true;
}
function renderSqlCell(row, maxLen) {
    return h('code', {
        style: 'font-family:var(--font-mono);font-size:11px;opacity:0.7;cursor:pointer;text-decoration:underline dotted;text-underline-offset:3px',
        onClick: () => showSqlDetail(row),
    }, truncate(row.sql_text, maxLen));
}
function SqlDetailModal() {
    const row = _sqlDetail.row;
    return h(NModal, {
        show: _sqlDetail.show, 'onUpdate:show': v => _sqlDetail.show = v,
        preset: 'card', title: 'SQL 详情',
        style: _isMobile.value ? 'width:95vw' : 'width:680px',
    }, () => h('div', [
        row ? h(NDescriptions, { bordered: true, column: 2, labelPlacement: _isMobile.value ? 'top' : 'left', size: 'small', style: 'margin-bottom:16px' }, () => [
            row.database_name ? h(NDescriptionsItem, { label: '数据库' }, () => row.database_name) : null,
            row.user ? h(NDescriptionsItem, { label: '用户' }, () => (row.user || '') + (row.host ? '@' + row.host : '')) : null,
            row.exec_sec != null ? h(NDescriptionsItem, { label: '执行耗时' }, () => h(NText, { type: 'error', strong: true }, () => row.exec_sec.toFixed(3) + 's')) : null,
            row.lock_sec != null ? h(NDescriptionsItem, { label: '锁等待' }, () => row.lock_sec.toFixed(3) + 's') : null,
            row.rows_examined != null ? h(NDescriptionsItem, { label: '扫描行数' }, () => String(row.rows_examined)) : null,
            row.db_name ? h(NDescriptionsItem, { label: '库名' }, () => row.db_name) : null,
            row.process_id ? h(NDescriptionsItem, { label: 'KILL 命令' }, () => h('code', { style: 'font-family:var(--font-mono);font-size:12px' }, 'KILL ' + row.process_id + ';')) : null,
            row.detected_at ? h(NDescriptionsItem, { label: '检测时间' }, () => formatTime(row.detected_at)) : null,
        ]) : null,
        h('div', { class: 'sql-detail-block' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:8px' }, [
                h(NText, { depth: 3, style: 'font-size:12px' }, () => 'SQL 语句'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => { copyText(_sqlDetail.sql); } }, () => '复制'),
            ]),
            h('pre', { class: 'sql-detail-code' }, _sqlDetail.sql),
        ]),
    ]));
}

// ============================================================
// Pages
// ============================================================

// --- Login ---
const LoginPage = defineComponent({
    setup() {
        const form = reactive({ username: '', password: '' });
        const loading = ref(false);
        const error = ref('');
        const authConfig = reactive({ github_enabled: false, password_login_enabled: true, github_client_id: '' });
        const message = useMessage();

        onMounted(async () => {
            try {
                const cfg = await api.get('/api/auth/config');
                Object.assign(authConfig, cfg);
            } catch {}
            const hash = window.location.hash;
            if (hash.includes('error=not_allowed')) error.value = '该 GitHub 账号未被授权登录';
            else if (hash.includes('error=oauth_failed')) error.value = 'GitHub 授权失败，请重试';
        });

        async function handleLogin() {
            loading.value = true;
            error.value = '';
            try {
                await api.post('/api/auth/login', form);
                window.location.hash = '#/dashboard';
            } catch (e) {
                error.value = e.message;
            } finally {
                loading.value = false;
            }
        }

        function githubLogin() {
            window.location.href = '/api/auth/github';
        }

        // SVG database icon
        const dbIcon = () => h('svg', { width: 48, height: 48, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.5', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', style: 'color:var(--login-accent)' }, [
            h('ellipse', { cx: 12, cy: 5, rx: 9, ry: 3 }),
            h('path', { d: 'M21 12c0 1.66-4 3-9 3s-9-1.34-9-3' }),
            h('path', { d: 'M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5' }),
        ]);

        return () => h('div', { class: 'login-page' }, [
            // Theme toggle on login page
            h('div', { style: 'position:absolute;top:16px;right:16px;z-index:10' }, [
                h(NButton, { quaternary: true, circle: true, onClick: toggleTheme, style: 'font-size:18px' }, () => themeIcon()),
            ]),
            h('div', { class: 'login-card-wrapper' }, [
                h('div', { class: 'login-card' }, [
                    // Logo area
                    h('div', { class: 'login-header' }, [
                        h('div', { class: 'login-logo' }, [dbIcon()]),
                        h('h1', { class: 'login-title' }, 'Ops Monitor'),
                        h('p', { class: 'login-subtitle' }, '运维监控管理平台'),
                    ]),

                    // Error alert
                    error.value ? h(NAlert, { type: 'error', style: 'margin-bottom:20px', closable: true, onClose: () => error.value = '' }, { default: () => error.value }) : null,

                    // Password login form
                    authConfig.password_login_enabled ? h('div', { class: 'login-form' }, [
                        h('div', { class: 'login-field' }, [
                            h(NInput, { value: form.username, 'onUpdate:value': v => form.username = v, placeholder: '用户名', size: 'large', round: true }),
                        ]),
                        h('div', { class: 'login-field' }, [
                            h(NInput, { value: form.password, 'onUpdate:value': v => form.password = v, type: 'password', showPasswordOn: 'click', placeholder: '密码', size: 'large', round: true, onKeyup: e => e.key === 'Enter' && handleLogin() }),
                        ]),
                        h(NButton, { type: 'primary', block: true, loading: loading.value, onClick: handleLogin, size: 'large', round: true, style: 'margin-top:4px' }, () => '登 录'),
                    ]) : null,

                    // GitHub login
                    authConfig.github_enabled ? h('div', [
                        authConfig.password_login_enabled ? h('div', { class: 'login-divider' }, [
                            h('span', null, '其他登录方式'),
                        ]) : null,
                        h('button', { class: 'github-btn', onClick: githubLogin }, [
                            h('svg', { viewBox: '0 0 16 16', width: 20, height: 20, fill: 'currentColor' }, [
                                h('path', { d: 'M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z' }),
                            ]),
                            'GitHub 登录'
                        ]),
                    ]) : null,

                    !authConfig.password_login_enabled && !authConfig.github_enabled ? h(NResult, { status: 'warning', title: '登录方式未配置', description: '请联系管理员' }) : null,
                ]),
            ]),
        ]);
    }
});

// --- Dashboard ---
const DashboardPage = defineComponent({
    setup() {
        const stats = ref(null);
        const loading = ref(true);
        const { connected, messages, stop } = useWebSocket('/ws/slow-queries');

        onMounted(async () => {
            try { stats.value = await api.get('/api/dashboard/stats'); } catch {}
            loading.value = false;
        });
        onUnmounted(stop);

        // Real-time: push new slow queries into dashboard
        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (!stats.value || !latest || latest.type !== 'slow_query' || !latest.data) return;
            stats.value.today_count++;
            stats.value.week_count++;
            if (!stats.value.recent_logs) stats.value.recent_logs = [];
            stats.value.recent_logs.unshift(latest.data);
            if (stats.value.recent_logs.length > 20) stats.value.recent_logs.length = 20;
            if (messages.value.length > 200) messages.value.splice(0, messages.value.length - 200);
        });

        const recentColumns = useColumns([
            { title: '时间', key: 'detected_at', width: 130, render: row => h('span', { style: 'font-size:12px;opacity:0.65' }, formatTime(row.detected_at)) },
            { title: '数据库', key: 'database_name', width: 120 },
            { title: '用户', key: 'user', width: 100, _hideOnMobile: true },
            { title: '耗时', key: 'exec_sec', width: 80, render: row => h(NText, { type: 'error', strong: true }, () => row.exec_sec.toFixed(1) + 's') },
            { title: 'SQL', key: 'sql_text', ellipsis: { tooltip: true }, _hideOnMobile: true, render: row => renderSqlCell(row, 80) },
        ]);

        function statCard(title, subtitle, items, link) {
            return h('div', {
                class: 'stat-card' + (link ? ' stat-card-clickable' : ''),
                onClick: link ? () => router.push(link) : undefined,
                style: link ? 'cursor:pointer' : '',
            }, [
                h('div', { class: 'stat-card-header' }, [
                    h('span', { class: 'stat-card-title' }, title),
                    subtitle ? h('span', { class: 'stat-card-subtitle' }, subtitle) : null,
                ]),
                h('div', { class: 'stat-card-body' }, items.map(item =>
                    h('div', {
                        class: 'stat-card-item' + (item.link ? ' stat-item-clickable' : ''),
                        onClick: item.link ? (e) => { e.stopPropagation(); router.push(item.link); } : undefined,
                        style: item.link ? 'cursor:pointer' : '',
                    }, [
                        h('div', { class: 'stat-card-value', style: item.color ? ('color:' + item.color) : '' }, String(item.value)),
                        h('div', { class: 'stat-card-label' }, item.label),
                    ])
                )),
            ]);
        }

        return () => h(NSpin, { show: loading.value }, () => stats.value ? h('div', { class: 'page-body' }, [
            h('div', { class: 'page-header' }, [
                h('h3', { class: 'page-title' }, '仪表盘'),
                h('div', { style: 'display:flex;align-items:center;gap:6px;font-size:12px;opacity:0.5' }, [
                    h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                    connected.value ? '实时监控中' : '连接断开'
                ]),
            ]),
            h('div', { class: 'stat-grid' }, [
                statCard('MySQL', '慢SQL监控', [
                    { label: '运行中', value: stats.value.running_dbs, color: '#18a058', link: '/databases' },
                    { label: '已配置', value: stats.value.total_dbs, color: '#2080f0', link: '/databases' },
                    { label: '今日慢SQL', value: stats.value.today_count, color: stats.value.today_count > 0 ? '#d03050' : '#999', link: '/slow-queries' },
                ], '/databases'),
                statCard('RocketMQ', '消息堆积监控', [
                    { label: '运行中', value: stats.value.rocketmq_running || 0, color: '#18a058', link: '/rocketmq' },
                    { label: '已配置', value: stats.value.rocketmq_configs || 0, color: '#2080f0', link: '/rocketmq' },
                    { label: '今日告警', value: stats.value.rocketmq_alerts_today || 0, color: (stats.value.rocketmq_alerts_today || 0) > 0 ? '#d03050' : '#999', link: '/rocketmq-alerts' },
                ], '/rocketmq'),
                statCard('健康检查', 'HTTP 端点监控', [
                    { label: '运行中', value: stats.value.health_checks_running || 0, color: '#18a058', link: '/health-checks' },
                    { label: '已配置', value: stats.value.health_checks || 0, color: '#2080f0', link: '/health-checks' },
                    { label: '今日异常', value: stats.value.health_check_errors_today || 0, color: (stats.value.health_check_errors_today || 0) > 0 ? '#d03050' : '#999', link: '/health-checks-logs' },
                ], '/health-checks'),
            ]),
            h('h4', { class: 'section-title' }, '最近慢SQL'),
            stats.value.recent_logs && stats.value.recent_logs.length > 0
                ? h(NDataTable, { columns: recentColumns.value, data: stats.value.recent_logs, bordered: false, size: 'small', maxHeight: 400, scrollX: _isMobile.value ? 400 : undefined, rowKey: row => row.id || row.detected_at })
                : h(NEmpty, { description: '暂无慢SQL记录' }),
        ]) : null);
    }
});

// --- Databases ---
const DatabasesPage = defineComponent({
    setup() {
        const databases = ref([]);
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const form = reactive({ name: '', host: '', port: 3306, user: '', password: '', interval_sec: 10, threshold_sec: 10 });
        const saving = ref(false);
        const message = useMessage();

        async function load() {
            loading.value = true;
            try { databases.value = await api.get('/api/databases'); } catch {}
            loading.value = false;
        }
        onMounted(load);

        function openAdd() {
            editingId.value = null;
            Object.assign(form, { name: '', host: '', port: 3306, user: '', password: '', interval_sec: 10, threshold_sec: 10 });
            showModal.value = true;
        }

        function openEdit(row) {
            editingId.value = row.id;
            Object.assign(form, { name: row.name, host: row.host, port: row.port, user: row.user, password: '', interval_sec: row.interval_sec, threshold_sec: row.threshold_sec });
            showModal.value = true;
        }

        function openClone(row) {
            editingId.value = null;
            Object.assign(form, { name: row.name + ' (副本)', host: row.host, port: row.port, user: row.user, password: '', interval_sec: row.interval_sec, threshold_sec: row.threshold_sec });
            showModal.value = true;
        }

        async function save() {
            saving.value = true;
            try {
                if (editingId.value) {
                    await api.put('/api/databases/' + editingId.value, form);
                    message.success('更新成功');
                } else {
                    await api.post('/api/databases', form);
                    message.success('创建成功');
                }
                showModal.value = false;
                await load();
            } catch (e) { message.error(e.message); }
            saving.value = false;
        }

        async function toggle(row) {
            try { await api.post('/api/databases/' + row.id + '/toggle'); await load(); } catch (e) { message.error(e.message); }
        }
        async function del(row) {
            try { await api.del('/api/databases/' + row.id); message.success('已删除'); await load(); } catch (e) { message.error(e.message); }
        }
        async function test(row) {
            try {
                const res = await api.post('/api/databases/' + row.id + '/test');
                res.ok ? message.success(res.message) : message.error(res.message);
            } catch (e) { message.error(e.message); }
        }

        const columns = useColumns([
            { title: '名称', key: 'name', render: row => h(NText, { strong: true }, () => row.name) },
            { title: '地址', key: 'host', _hideOnMobile: true, render: row => h(NText, { depth: 3, style: 'font-size:12px' }, () => row.host + ':' + row.port) },
            { title: '用户', key: 'user', _hideOnMobile: true },
            { title: '间隔/阈值', key: 'interval', _hideOnMobile: true, render: row => h(NText, { depth: 3, style: 'font-size:12px' }, () => row.interval_sec + 's / ' + row.threshold_sec + 's') },
            { title: '状态', key: 'status', width: 90, render: row => row.running ? h(NTag, { type: 'success', size: 'small' }, () => '运行中') : row.enabled ? h(NTag, { type: 'warning', size: 'small' }, () => '已启用') : h(NTag, { size: 'small' }, () => '已禁用') },
            { title: '操作', key: 'actions', width: _isMobile.value ? 160 : 310, render: row => h(NSpace, { size: 'small', wrap: _isMobile.value }, () => {
                const btns = [
                    h(NButton, { size: 'small', secondary: true, onClick: () => toggle(row) }, () => row.enabled ? '禁用' : '启用'),
                    h(NButton, { size: 'small', secondary: true, onClick: () => openEdit(row) }, () => '编辑'),
                    h(NButton, { size: 'small', secondary: true, onClick: () => openClone(row) }, () => '复制'),
                ];
                if (!_isMobile.value) btns.push(h(NButton, { size: 'small', secondary: true, onClick: () => test(row) }, () => '测试'));
                btns.push(h(NPopconfirm, { onPositiveClick: () => del(row) }, { trigger: () => h(NButton, { size: 'small', secondary: true, type: 'error' }, () => '删除'), default: () => '确定删除？' }));
                return btns;
            }) },
        ]);

        const gridCols = computed(() => _isMobile.value ? 1 : 2);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, '数据库管理'),
                h(NButton, { type: 'primary', onClick: openAdd, size: _isMobile.value ? 'small' : 'medium' }, () => '+ 添加'),
            ]),
            h(NDataTable, { columns: columns.value, data: databases.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 400 : undefined }),
            h(NModal, { show: showModal.value, 'onUpdate:show': v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑数据库' : '添加数据库', style: _isMobile.value ? 'width:95vw' : 'width:620px', segmented: true }, () => h(NForm, { model: form, labelPlacement: _isMobile.value ? 'top' : 'left', labelWidth: _isMobile.value ? undefined : 110 }, [
                h(NFormItem, { label: '名称' }, () => h(NInput, { value: form.name, 'onUpdate:value': v => form.name = v, placeholder: '如: 生产数据库' })),
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '主机' }, () => h(NInput, { value: form.host, 'onUpdate:value': v => form.host = v, placeholder: '127.0.0.1' }))),
                    h(NGi, null, () => h(NFormItem, { label: '端口' }, () => h(NInputNumber, { value: form.port, 'onUpdate:value': v => form.port = v, min: 1, max: 65535 }))),
                ]),
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '用户名' }, () => h(NInput, { value: form.user, 'onUpdate:value': v => form.user = v }))),
                    h(NGi, null, () => h(NFormItem, { label: '密码' }, () => h(NInput, { value: form.password, 'onUpdate:value': v => form.password = v, type: 'password', placeholder: editingId.value ? '留空不修改' : '' }))),
                ]),
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '监控间隔(秒)' }, () => h(NInputNumber, { value: form.interval_sec, 'onUpdate:value': v => form.interval_sec = v, min: 1 }))),
                    h(NGi, null, () => h(NFormItem, { label: '慢SQL阈值(秒)' }, () => h(NInputNumber, { value: form.threshold_sec, 'onUpdate:value': v => form.threshold_sec = v, min: 1 }))),
                ]),
                h(NButton, { type: 'primary', block: true, loading: saving.value, onClick: save, style: 'margin-top:8px' }, () => editingId.value ? '保存' : '创建'),
            ])),
        ]);
    }
});

// --- Notifications ---
const NotificationsPage = defineComponent({
    setup() {
        const list = ref([]);
        const databases = ref([]);
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const form = reactive({ type: 'dingtalk', database_id: null, webhook: '', secret: '', smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', email_from: '', email_to: '' });
        const saving = ref(false);
        const message = useMessage();

        async function load() {
            loading.value = true;
            try {
                list.value = await api.get('/api/notifications');
                databases.value = await api.get('/api/databases-simple');
            } catch {}
            loading.value = false;
        }
        onMounted(load);

        const typeOptions = [
            { label: '钉钉', value: 'dingtalk' },
            { label: '飞书', value: 'feishu' },
            { label: '邮件', value: 'email' },
        ];
        const dbOptions = computed(() => [
            { label: '全局（所有数据库）', value: null },
            ...databases.value.map(d => ({ label: d.name, value: d.id }))
        ]);

        function openAdd() {
            editingId.value = null;
            Object.assign(form, { type: 'dingtalk', database_id: null, webhook: '', secret: '', smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', email_from: '', email_to: '' });
            showModal.value = true;
        }
        function fillFormFromRow(row) {
            form.type = row.type;
            form.database_id = row.database_id;
            const cfg = typeof row.config_json === 'string' ? JSON.parse(row.config_json) : row.config_json;
            if (row.type === 'dingtalk' || row.type === 'feishu') {
                form.webhook = cfg.webhook || '';
                form.secret = cfg.secret || '';
            } else if (row.type === 'email') {
                form.smtp_host = cfg.smtp_host || '';
                form.smtp_port = cfg.smtp_port || 587;
                form.smtp_username = cfg.username || '';
                form.smtp_password = cfg.password || '';
                form.email_from = cfg.from || '';
                form.email_to = cfg.to || '';
            }
        }
        function openEdit(row) {
            editingId.value = row.id;
            fillFormFromRow(row);
            showModal.value = true;
        }
        function openClone(row) {
            editingId.value = null;
            fillFormFromRow(row);
            showModal.value = true;
        }
        async function save() {
            saving.value = true;
            try {
                if (editingId.value) {
                    await api.put('/api/notifications/' + editingId.value, form);
                    message.success('更新成功');
                } else {
                    await api.post('/api/notifications', form);
                    message.success('创建成功');
                }
                showModal.value = false;
                await load();
            } catch (e) { message.error(e.message); }
            saving.value = false;
        }
        async function del(row) {
            try { await api.del('/api/notifications/' + row.id); message.success('已删除'); await load(); } catch (e) { message.error(e.message); }
        }
        async function test(row) {
            try {
                const res = await api.post('/api/notifications/' + row.id + '/test');
                res.ok ? message.success(res.message) : message.error(res.message);
            } catch (e) { message.error(e.message); }
        }

        const columns = useColumns([
            { title: '类型', key: 'type', width: 80, render: row => h(NTag, { type: 'info', size: 'small' }, () => row.type === 'dingtalk' ? '钉钉' : row.type === 'feishu' ? '飞书' : '邮件') },
            { title: '关联数据库', key: 'database_name', _hideOnMobile: true },
            { title: '配置摘要', key: 'config_summary', render: row => h(NText, { depth: 3, style: 'font-size:12px' }, () => row.config_summary) },
            { title: '状态', key: 'enabled', width: 70, _hideOnMobile: true, render: row => row.enabled ? h(NTag, { type: 'success', size: 'small' }, () => '启用') : h(NTag, { size: 'small' }, () => '禁用') },
            { title: '操作', key: 'actions', width: _isMobile.value ? 140 : 250, render: row => h(NSpace, { size: 'small' }, () => [
                h(NButton, { size: 'small', secondary: true, onClick: () => openEdit(row) }, () => '编辑'),
                h(NButton, { size: 'small', secondary: true, onClick: () => openClone(row) }, () => '复制'),
                !_isMobile.value ? h(NButton, { size: 'small', secondary: true, onClick: () => test(row) }, () => '测试') : null,
                h(NPopconfirm, { onPositiveClick: () => del(row) }, { trigger: () => h(NButton, { size: 'small', secondary: true, type: 'error' }, () => '删除'), default: () => '确定删除？' }),
            ].filter(Boolean)) },
        ]);

        const gridCols = computed(() => _isMobile.value ? 1 : 2);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, '通知配置'),
                h(NButton, { type: 'primary', onClick: openAdd, size: _isMobile.value ? 'small' : 'medium' }, () => '+ 添加'),
            ]),
            h(NDataTable, { columns: columns.value, data: list.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 350 : undefined }),
            h(NModal, { show: showModal.value, 'onUpdate:show': v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑通知' : '添加通知', style: _isMobile.value ? 'width:95vw' : 'width:520px', segmented: true }, () => h(NForm, { model: form, labelPlacement: _isMobile.value ? 'top' : 'left', labelWidth: _isMobile.value ? undefined : 100 }, [
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '通知类型' }, () => h(NSelect, { value: form.type, 'onUpdate:value': v => form.type = v, options: typeOptions }))),
                    h(NGi, null, () => h(NFormItem, { label: '关联数据库' }, () => h(NSelect, { value: form.database_id, 'onUpdate:value': v => form.database_id = v, options: dbOptions.value, clearable: true }))),
                ]),
                form.type !== 'email' ? h('div', [
                    h(NFormItem, { label: 'Webhook URL' }, () => h(NInput, { value: form.webhook, 'onUpdate:value': v => form.webhook = v, placeholder: 'https://...' })),
                    h(NFormItem, { label: '签名密钥' }, () => h(NInput, { value: form.secret, 'onUpdate:value': v => form.secret = v, placeholder: '可选' })),
                ]) : h('div', [
                    h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                        h(NGi, null, () => h(NFormItem, { label: 'SMTP 主机' }, () => h(NInput, { value: form.smtp_host, 'onUpdate:value': v => form.smtp_host = v }))),
                        h(NGi, null, () => h(NFormItem, { label: 'SMTP 端口' }, () => h(NInputNumber, { value: form.smtp_port, 'onUpdate:value': v => form.smtp_port = v }))),
                    ]),
                    h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                        h(NGi, null, () => h(NFormItem, { label: '用户名' }, () => h(NInput, { value: form.smtp_username, 'onUpdate:value': v => form.smtp_username = v }))),
                        h(NGi, null, () => h(NFormItem, { label: '密码' }, () => h(NInput, { value: form.smtp_password, 'onUpdate:value': v => form.smtp_password = v, type: 'password' }))),
                    ]),
                    h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                        h(NGi, null, () => h(NFormItem, { label: '发件人' }, () => h(NInput, { value: form.email_from, 'onUpdate:value': v => form.email_from = v }))),
                        h(NGi, null, () => h(NFormItem, { label: '收件人' }, () => h(NInput, { value: form.email_to, 'onUpdate:value': v => form.email_to = v, placeholder: '逗号分隔' }))),
                    ]),
                ]),
                h(NButton, { type: 'primary', block: true, loading: saving.value, onClick: save, style: 'margin-top:8px' }, () => editingId.value ? '保存' : '创建'),
            ])),
        ]);
    }
});

// --- Slow Queries ---
const SlowQueriesPage = defineComponent({
    setup() {
        const data = ref({ logs: [], total: 0, page: 1, total_pages: 0 });
        const databases = ref([]);
        const loading = ref(true);
        const filterDB = ref(null);
        const page = ref(1);

        const { connected, messages, stop } = useWebSocket('/ws/slow-queries');
        onUnmounted(stop);

        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (latest && latest.type === 'slow_query' && latest.data) {
                if (!filterDB.value || latest.database_id === filterDB.value) {
                    data.value.logs.unshift(latest.data);
                    if (data.value.logs.length > 200) data.value.logs.length = 200;
                    data.value.total++;
                }
            }
            if (messages.value.length > 500) messages.value.splice(0, messages.value.length - 500);
        });

        async function load() {
            loading.value = true;
            try {
                let url = '/api/slow-queries?page=' + page.value;
                if (filterDB.value) url += '&database_id=' + filterDB.value;
                data.value = await api.get(url);
                databases.value = await api.get('/api/databases-simple');
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch([page, filterDB], () => load());

        const dbOptions = computed(() => [
            { label: '全部', value: null },
            ...databases.value.map(d => ({ label: d.name, value: d.id }))
        ]);

        const columns = useColumns([
            { title: '检测时间', key: 'detected_at', width: 140, render: row => h(NText, { depth: 3, style: 'font-size:12px' }, () => formatTime(row.detected_at)) },
            { title: '数据库', key: 'database_name', width: 100 },
            { title: '用户@主机', key: 'user', width: 150, _hideOnMobile: true, render: row => h(NText, { depth: 3, style: 'font-size:12px' }, () => (row.user || '') + '@' + (row.host || '')) },
            { title: '库名', key: 'db_name', width: 160, _hideOnMobile: true, ellipsis: { tooltip: true } },
            { title: '耗时', key: 'exec_sec', width: 70, render: row => h(NText, { type: 'error', strong: true }, () => row.exec_sec.toFixed(1) + 's') },
            { title: '锁等待', key: 'lock_sec', width: 70, _hideOnMobile: true, render: row => row.lock_sec.toFixed(1) + 's' },
            { title: '扫描行', key: 'rows_examined', width: 80, _hideOnMobile: true },
            { title: 'SQL', key: 'sql_text', ellipsis: { tooltip: true }, render: row => renderSqlCell(row, _isMobile.value ? 30 : 60) },
            { title: 'KILL', key: 'kill', width: 100, _hideOnMobile: true, render: row => h('code', { style: 'font-family:var(--font-mono);font-size:11px;opacity:0.5' }, 'KILL ' + row.process_id + ';') },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: _isMobile.value ? 'margin-bottom:12px' : 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px;margin-bottom:' + (_isMobile.value ? '8px' : '0') }, [
                    h('h3', { class: 'page-title' }, '慢SQL日志'),
                    h(NText, { depth: 3 }, () => '共 ' + data.value.total + ' 条'),
                    h('div', { style: 'display:flex;align-items:center;gap:4px;font-size:12px;opacity:0.5' }, [
                        h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                        connected.value ? '实时' : '断开'
                    ]),
                ]),
                h(NSelect, { value: filterDB.value, 'onUpdate:value': v => { filterDB.value = v; page.value = 1; }, options: dbOptions.value, style: _isMobile.value ? 'width:100%' : 'width:180px', placeholder: '筛选数据库', clearable: true, size: 'small' }),
            ]),
            h(NDataTable, { columns: columns.value, data: data.value.logs || [], bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 500 : undefined }),
            data.value.total_pages > 1 ? h('div', { style: 'margin-top:16px;display:flex;justify-content:center' }, [
                h(NPagination, { page: page.value, 'onUpdate:page': v => page.value = v, pageCount: data.value.total_pages, size: 'small' }),
            ]) : null,
        ]);
    }
});

// --- Monitor Logs ---
const MonitorLogsPage = defineComponent({
    setup() {
        const paused = ref(false);
        const logEntries = ref([]);
        const maxEntries = 500;

        const { connected, messages, stop, clear } = useWebSocket('/ws/monitor-logs');
        onUnmounted(stop);

        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (!latest) return;
            logEntries.value.push(latest);
            if (logEntries.value.length > maxEntries) logEntries.value = logEntries.value.slice(-maxEntries);
            if (!paused.value) {
                nextTick(() => {
                    const el = document.getElementById('log-scroll');
                    if (el) el.scrollTop = el.scrollHeight;
                });
            }
        });

        function clearLogs() { logEntries.value = []; clear(); }
        function getIcon(type) {
            switch(type) {
                case 'checking': return '...';
                case 'no_queries': return '\u2713';
                case 'found_queries': return '\u26a0';
                case 'notified': return '\u2709';
                case 'error': return '\u2717';
                default: return '\u2022';
            }
        }
        function getMsgClass(type) {
            switch(type) {
                case 'checking': return 'log-msg-checking';
                case 'no_queries': return 'log-msg-ok';
                case 'found_queries': return 'log-msg-found';
                case 'notified': return 'log-msg-notify';
                case 'error': return 'log-msg-error';
                default: return 'log-msg-info';
            }
        }

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px' }, [
                    h('h3', { class: 'page-title' }, '监控日志'),
                    h('div', { style: 'display:flex;align-items:center;gap:4px;font-size:12px;opacity:0.5' }, [
                        h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                        connected.value ? '已连接' : '已断开'
                    ]),
                ]),
                h(NSpace, { size: 'small' }, () => [
                    h(NButton, { size: 'small', secondary: true, onClick: () => paused.value = !paused.value }, () => paused.value ? '继续' : '暂停'),
                    h(NButton, { size: 'small', secondary: true, onClick: clearLogs }, () => '清除'),
                ]),
            ]),
            h('div', { id: 'log-scroll', class: 'log-container' },
                logEntries.value.length === 0
                    ? h('div', { class: 'log-entry', style: 'opacity:0.4' }, '等待监控事件...')
                    : logEntries.value.map((entry, i) => h('div', { class: 'log-entry', key: i }, [
                        h('span', { class: 'log-time' }, '[' + formatTimeShort(entry.timestamp) + '] '),
                        h('span', { class: 'log-db' }, entry.db_name + ' '),
                        h('span', { class: getMsgClass(entry.type) }, getIcon(entry.type) + ' ' + entry.message),
                    ]))
            ),
        ]);
    }
});

// --- Settings ---
const SettingsPage = defineComponent({
    setup() {
        const settings = reactive({ github_client_id: '', github_client_secret: '', github_enabled: '0', password_login_enabled: '1' });
        const users = ref([]);
        const loading = ref(true);
        const saving = ref(false);
        const newUser = reactive({ github_login: '', role: 'member' });
        const message = useMessage();

        async function load() {
            loading.value = true;
            try {
                const s = await api.get('/api/settings');
                Object.assign(settings, s);
                users.value = await api.get('/api/users');
            } catch {}
            loading.value = false;
        }
        onMounted(load);

        async function saveSettings() {
            saving.value = true;
            try {
                await api.put('/api/settings', {
                    github_client_id: settings.github_client_id,
                    github_client_secret: settings.github_client_secret,
                    github_enabled: settings.github_enabled,
                    password_login_enabled: settings.password_login_enabled,
                });
                message.success('设置已保存');
            } catch (e) { message.error(e.message); }
            saving.value = false;
        }
        async function addUser() {
            if (!newUser.github_login) { message.warning('请输入 GitHub 用户名'); return; }
            try {
                await api.post('/api/users', newUser);
                message.success('已添加');
                newUser.github_login = '';
                await load();
            } catch (e) { message.error(e.message); }
        }
        async function delUser(row) {
            try { await api.del('/api/users/' + row.id); message.success('已删除'); await load(); } catch (e) { message.error(e.message); }
        }

        const userColumns = useColumns([
            { title: '用户名', key: 'username' },
            { title: 'GitHub', key: 'github_login' },
            { title: '角色', key: 'role', width: 80, render: row => h(NTag, { type: row.role === 'admin' ? 'warning' : 'info', size: 'small' }, () => row.role) },
            { title: '操作', key: 'actions', width: 80, render: row => h(NPopconfirm, { onPositiveClick: () => delUser(row) }, { trigger: () => h(NButton, { size: 'small', secondary: true, type: 'error' }, () => '删除'), default: () => '确定删除？' }) },
        ]);

        return () => h(NSpin, { show: loading.value }, () => h('div', [
            h('h3', { class: 'page-title', style: 'margin-bottom:20px' }, '系统设置'),
            h(NCard, { title: 'GitHub OAuth', size: 'small', style: 'margin-bottom:20px' }, () => h(NForm, { model: settings, labelPlacement: _isMobile.value ? 'top' : 'left', labelWidth: _isMobile.value ? undefined : 140 }, [
                h(NFormItem, { label: 'Client ID' }, () => h(NInput, { value: settings.github_client_id, 'onUpdate:value': v => settings.github_client_id = v, placeholder: 'GitHub OAuth App Client ID' })),
                h(NFormItem, { label: 'Client Secret' }, () => h(NInput, { value: settings.github_client_secret, 'onUpdate:value': v => settings.github_client_secret = v, type: 'password', placeholder: '留空不修改' })),
                h(NFormItem, { label: '启用 GitHub 登录' }, () => h(NSwitch, { value: settings.github_enabled === '1', 'onUpdate:value': v => settings.github_enabled = v ? '1' : '0' })),
                h(NFormItem, { label: '启用密码登录' }, () => h(NSwitch, { value: settings.password_login_enabled !== '0', 'onUpdate:value': v => settings.password_login_enabled = v ? '1' : '0' })),
                h(NButton, { type: 'primary', loading: saving.value, onClick: saveSettings }, () => '保存设置'),
            ])),
            h(NCard, { title: 'GitHub 授权用户', size: 'small' }, () => h('div', [
                h('div', { style: _isMobile.value ? 'display:flex;flex-direction:column;gap:8px;margin-bottom:16px' : 'display:flex;gap:8px;margin-bottom:16px' }, [
                    h(NInput, { value: newUser.github_login, 'onUpdate:value': v => newUser.github_login = v, placeholder: 'GitHub 用户名', style: _isMobile.value ? 'width:100%' : 'width:200px', onKeyup: e => e.key === 'Enter' && addUser() }),
                    h('div', { style: 'display:flex;gap:8px' }, [
                        h(NSelect, { value: newUser.role, 'onUpdate:value': v => newUser.role = v, options: [{ label: '成员', value: 'member' }, { label: '管理员', value: 'admin' }], style: 'width:120px' }),
                        h(NButton, { type: 'primary', onClick: addUser }, () => '添加'),
                    ]),
                ]),
                h(NDataTable, { columns: userColumns.value, data: users.value, bordered: false, size: 'small' }),
            ])),
        ]));
    }
});

// ============================================================
// Utility
// ============================================================
function formatTime(t) {
    if (!t) return '';
    const d = new Date(t);
    return (d.getMonth()+1).toString().padStart(2,'0') + '-' +
           d.getDate().toString().padStart(2,'0') + ' ' +
           d.getHours().toString().padStart(2,'0') + ':' +
           d.getMinutes().toString().padStart(2,'0') + ':' +
           d.getSeconds().toString().padStart(2,'0');
}

function formatTimeShort(t) {
    if (!t) return '';
    const d = new Date(t);
    return d.getHours().toString().padStart(2,'0') + ':' +
           d.getMinutes().toString().padStart(2,'0') + ':' +
           d.getSeconds().toString().padStart(2,'0');
}

function truncate(s, n) {
    if (!s) return '';
    return s.length <= n ? s : s.substring(0, n) + '...';
}

function copyText(text) {
    function onDone() { window.$message && window.$message.success('已复制到剪贴板'); }
    function onFail() { window.$message && window.$message.error('复制失败'); }
    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(onDone).catch(onFail);
    } else {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.cssText = 'position:fixed;left:-9999px';
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand('copy'); onDone(); } catch { onFail(); }
        document.body.removeChild(ta);
    }
}

// --- RocketMQ ---
const RocketMQPage = defineComponent({
    setup() {
        const configs = ref([]);
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const form = reactive({ name: '', dashboard_url: '', username: '', password: '', consumer_group: '', topic: '', threshold: 1000, interval_sec: 30 });
        const saving = ref(false);
        const message = useMessage();

        async function load() {
            loading.value = true;
            try { configs.value = await api.get('/api/rocketmq'); } catch {}
            loading.value = false;
        }
        onMounted(load);

        function openAdd() {
            editingId.value = null;
            Object.assign(form, { name: '', dashboard_url: '', username: '', password: '', consumer_group: '', topic: '', threshold: 1000, interval_sec: 30 });
            showModal.value = true;
        }
        function openEdit(row) {
            editingId.value = row.id;
            Object.assign(form, { name: row.name, dashboard_url: row.dashboard_url, username: row.username, password: '', consumer_group: row.consumer_group, topic: row.topic, threshold: row.threshold, interval_sec: row.interval_sec });
            showModal.value = true;
        }
        function openClone(row) {
            editingId.value = null;
            Object.assign(form, { name: row.name + ' (副本)', dashboard_url: row.dashboard_url, username: row.username, password: '', consumer_group: row.consumer_group, topic: row.topic, threshold: row.threshold, interval_sec: row.interval_sec });
            showModal.value = true;
        }
        async function save() {
            if (!form.name || !form.dashboard_url || !form.consumer_group || !form.topic) { message.error('请填写必填项'); return; }
            saving.value = true;
            try {
                if (editingId.value) await api.put('/api/rocketmq/' + editingId.value, form);
                else await api.post('/api/rocketmq', form);
                showModal.value = false;
                message.success(editingId.value ? '已更新' : '已创建');
                load();
            } catch (e) { message.error(e.message || '保存失败'); }
            saving.value = false;
        }
        async function del(row) {
            try { await api.del('/api/rocketmq/' + row.id); message.success('已删除'); load(); } catch (e) { message.error(e.message); }
        }
        async function toggle(row) {
            try { await api.post('/api/rocketmq/' + row.id + '/toggle'); load(); } catch (e) { message.error(e.message); }
        }
        async function test(row) {
            try {
                const res = await api.post('/api/rocketmq/' + row.id + '/test');
                res.ok ? message.success(res.message) : message.error(res.message);
            } catch (e) { message.error(e.message); }
        }

        const columns = useColumns([
            { title: '名称', key: 'name', width: 120 },
            { title: 'Dashboard', key: 'dashboard_url', ellipsis: { tooltip: true }, _hideOnMobile: true },
            { title: '消费组', key: 'consumer_group', width: 140, ellipsis: { tooltip: true } },
            { title: 'Topic', key: 'topic', width: 120, ellipsis: { tooltip: true }, _hideOnMobile: true },
            { title: '阈值', key: 'threshold', width: 80, _hideOnMobile: true },
            { title: '状态', key: 'status', width: 100, render: row => h(NSpace, { size: 4 }, () => [
                h(NTag, { size: 'small', type: row.enabled ? 'success' : 'default' }, () => row.enabled ? '启用' : '禁用'),
                row.running ? h(NBadge, { dot: true, type: 'success' }) : null,
            ])},
            { title: '操作', key: 'actions', width: _isMobile.value ? 180 : 300, render: row => h(NSpace, { size: 'small' }, () => [
                h(NButton, { size: 'tiny', secondary: true, onClick: () => openEdit(row) }, () => '编辑'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => openClone(row) }, () => '复制'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => toggle(row) }, () => row.enabled ? '禁用' : '启用'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => test(row) }, () => '测试'),
                h(NPopconfirm, { onPositiveClick: () => del(row) }, { trigger: () => h(NButton, { size: 'tiny', type: 'error', secondary: true }, () => '删除'), default: () => '确认删除？' }),
            ])},
        ]);

        const gridCols = computed(() => _isMobile.value ? 1 : 2);
        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, 'RocketMQ 监控'),
                h(NButton, { type: 'primary', size: 'small', onClick: openAdd }, () => '+ 新增'),
            ]),
            h(NDataTable, { columns: columns.value, data: configs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 500 : undefined }),
            h(NModal, { show: showModal.value, onUpdateShow: v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑配置' : '新增配置', style: _isMobile.value ? 'width:95vw' : 'width:680px' }, () =>
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '名称', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.name, onUpdateValue: v => form.name = v, placeholder: '如: 订单系统MQ' }))),
                    h(NGi, null, () => h(NFormItem, { label: 'Dashboard URL', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.dashboard_url, onUpdateValue: v => form.dashboard_url = v, placeholder: 'http://host:port' }))),
                    h(NGi, null, () => h(NFormItem, { label: '用户名', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.username, onUpdateValue: v => form.username = v, placeholder: '可选' }))),
                    h(NGi, null, () => h(NFormItem, { label: '密码', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.password, onUpdateValue: v => form.password = v, type: 'password', showPasswordOn: 'click', placeholder: editingId.value ? '留空不修改' : '可选' }))),
                    h(NGi, null, () => h(NFormItem, { label: '消费组', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.consumer_group, onUpdateValue: v => form.consumer_group = v, placeholder: 'ConsumerGroup 名称' }))),
                    h(NGi, null, () => h(NFormItem, { label: 'Topic', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.topic, onUpdateValue: v => form.topic = v, placeholder: 'Topic 名称' }))),
                    h(NGi, null, () => h(NFormItem, { label: '堆积阈值', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInputNumber, { value: form.threshold, onUpdateValue: v => form.threshold = v, min: 1 }))),
                    h(NGi, null, () => h(NFormItem, { label: '检查间隔(秒)', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInputNumber, { value: form.interval_sec, onUpdateValue: v => form.interval_sec = v, min: 5 }))),
                    h(NGi, { span: gridCols.value }, () => h(NButton, { type: 'primary', block: true, loading: saving.value, onClick: save }, () => '保存')),
                ])
            ),
        ]);
    }
});

// --- RocketMQ Alerts ---
const RocketMQAlertsPage = defineComponent({
    setup() {
        const alerts = ref([]);
        const total = ref(0);
        const page = ref(1);
        const pageSize = 20;
        const loading = ref(true);
        const { connected, messages, stop } = useWebSocket('/ws/rocketmq-logs');
        onUnmounted(stop);

        async function load() {
            loading.value = true;
            try {
                const res = await api.get('/api/rocketmq/alerts?page=' + page.value + '&page_size=' + pageSize);
                alerts.value = res.data || [];
                total.value = res.total || 0;
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch(page, load);

        // Live updates from WebSocket
        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (latest && latest.type === 'rocketmq_alert' && page.value === 1) {
                load();
            }
        });

        const columns = useColumns([
            { title: '时间', key: 'detected_at', width: 150, render: row => h('span', { style: 'font-size:12px;opacity:0.65' }, formatTime(row.detected_at)) },
            { title: '配置', key: 'config_name', width: 120 },
            { title: '消费组', key: 'consumer_group', width: 140, _hideOnMobile: true },
            { title: 'Topic', key: 'topic', width: 120 },
            { title: '堆积量', key: 'diff_total', width: 100, render: row => h(NText, { type: 'error', strong: true }, () => String(row.diff_total)) },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px' }, [
                    h('h3', { class: 'page-title' }, 'MQ 告警记录'),
                    h('div', { style: 'display:flex;align-items:center;gap:4px;font-size:12px;opacity:0.5' }, [
                        h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                        connected.value ? '实时' : '离线'
                    ]),
                ]),
            ]),
            h(NDataTable, { columns: columns.value, data: alerts.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 400 : undefined }),
            total.value > pageSize ? h('div', { style: 'margin-top:16px;display:flex;justify-content:flex-end' },
                h(NPagination, { page: page.value, pageSize, itemCount: total.value, onUpdatePage: p => page.value = p })
            ) : null,
        ]);
    }
});

// --- Audit Logs ---
const AuditLogsPage = defineComponent({
    setup() {
        const logs = ref([]);
        const total = ref(0);
        const page = ref(1);
        const pageSize = 50;
        const loading = ref(true);

        async function load() {
            loading.value = true;
            try {
                const res = await api.get('/api/audit-logs?page=' + page.value + '&page_size=' + pageSize);
                logs.value = res.data || [];
                total.value = res.total || 0;
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch(page, load);

        const actionTagType = (action) => {
            const map = { create: 'success', update: 'warning', delete: 'error', toggle: 'info', login: 'success', logout: 'default' };
            return map[action] || 'default';
        };

        const columns = useColumns([
            { title: '时间', key: 'created_at', width: 150, render: row => h('span', { style: 'font-size:12px;opacity:0.65' }, formatTime(row.created_at)) },
            { title: '操作人', key: 'user', width: 100 },
            { title: '操作', key: 'action', width: 80, render: row => h(NTag, { type: actionTagType(row.action), size: 'small', bordered: false }, () => row.action) },
            { title: '对象', key: 'target', width: 100 },
            { title: '详情', key: 'detail', ellipsis: { tooltip: true } },
            { title: 'IP', key: 'ip', width: 130, _hideOnMobile: true },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('h3', { class: 'page-title', style: 'margin-bottom:16px' }, '操作记录'),
            h(NDataTable, { columns: columns.value, data: logs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 500 : undefined }),
            total.value > pageSize ? h('div', { style: 'margin-top:16px;display:flex;justify-content:flex-end' },
                h(NPagination, { page: page.value, pageSize, itemCount: total.value, onUpdatePage: p => page.value = p })
            ) : null,
        ]);
    }
});

// --- Health Checks ---
const HealthChecksPage = defineComponent({
    setup() {
        const checks = ref([]);
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const message = useMessage();
        const form = reactive({
            name: '', url: '', method: 'GET', headers_json: '{}', body: '',
            expected_status: 200, expected_field: '', expected_value: '',
            timeout_sec: 10, interval_sec: 30
        });

        async function load() {
            loading.value = true;
            try { checks.value = await api.get('/api/health-checks'); } catch {}
            loading.value = false;
        }
        onMounted(load);

        function openAdd() {
            editingId.value = null;
            Object.assign(form, { name: '', url: '', method: 'GET', headers_json: '{}', body: '', expected_status: 200, expected_field: '', expected_value: '', timeout_sec: 10, interval_sec: 30 });
            showModal.value = true;
        }
        function openEdit(row) {
            editingId.value = row.id;
            Object.assign(form, { name: row.name, url: row.url, method: row.method, headers_json: row.headers_json || '{}', body: row.body || '', expected_status: row.expected_status, expected_field: row.expected_field || '', expected_value: row.expected_value || '', timeout_sec: row.timeout_sec, interval_sec: row.interval_sec });
            showModal.value = true;
        }
        function openClone(row) {
            editingId.value = null;
            Object.assign(form, { name: row.name + ' (副本)', url: row.url, method: row.method, headers_json: row.headers_json || '{}', body: row.body || '', expected_status: row.expected_status, expected_field: row.expected_field || '', expected_value: row.expected_value || '', timeout_sec: row.timeout_sec, interval_sec: row.interval_sec });
            showModal.value = true;
        }
        async function save() {
            try {
                if (editingId.value) {
                    await api.put('/api/health-checks/' + editingId.value, form);
                } else {
                    await api.post('/api/health-checks', form);
                }
                showModal.value = false;
                load();
            } catch (e) { message.error(e.message || '保存失败'); }
        }
        async function toggle(row) {
            try { await api.post('/api/health-checks/' + row.id + '/toggle'); load(); } catch {}
        }
        async function test(row) {
            try {
                const res = await api.post('/api/health-checks/' + row.id + '/test');
                if (res.ok) message.success('状态: UP (' + res.latency_ms + 'ms)');
                else message.error('状态: DOWN - ' + (res.error || 'HTTP ' + res.http_status));
            } catch (e) { message.error(e.message); }
        }
        async function remove(row) {
            try { await api.del('/api/health-checks/' + row.id); load(); } catch {}
        }

        const columns = useColumns([
            { title: '名称', key: 'name', width: 120 },
            { title: 'URL', key: 'url', ellipsis: { tooltip: true }, _hideOnMobile: true },
            { title: '方法', key: 'method', width: 70 },
            { title: '间隔', key: 'interval_sec', width: 70, render: row => row.interval_sec + 's', _hideOnMobile: true },
            { title: '状态', key: 'enabled', width: 100, render: row => h('div', { style: 'display:flex;gap:4px' }, [
                h(NTag, { type: row.enabled ? 'success' : 'default', size: 'small', bordered: false }, () => row.enabled ? '启用' : '停用'),
                row.running ? h(NTag, { type: 'info', size: 'small', bordered: false }, () => '运行中') : null,
            ])},
            { title: '操作', key: 'actions', width: 280, render: row => h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap' }, [
                h(NButton, { size: 'tiny', secondary: true, onClick: () => openEdit(row) }, () => '编辑'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => openClone(row) }, () => '复制'),
                h(NButton, { size: 'tiny', secondary: true, type: row.enabled ? 'warning' : 'success', onClick: () => toggle(row) }, () => row.enabled ? '禁用' : '启用'),
                h(NButton, { size: 'tiny', secondary: true, type: 'info', onClick: () => test(row) }, () => '测试'),
                h(NPopconfirm, { onPositiveClick: () => remove(row) }, { trigger: () => h(NButton, { size: 'tiny', secondary: true, type: 'error' }, () => '删除'), default: () => '确认删除？' }),
            ])},
        ]);

        const methodOptions = [
            { label: 'GET', value: 'GET' },
            { label: 'POST', value: 'POST' },
            { label: 'PUT', value: 'PUT' },
            { label: 'HEAD', value: 'HEAD' },
        ];

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, '健康检查'),
                h(NButton, { type: 'primary', size: 'small', onClick: openAdd }, () => '+ 添加'),
            ]),
            h(NDataTable, { columns: columns.value, data: checks.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 500 : undefined }),
            h(NModal, { show: showModal.value, 'onUpdate:show': v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑健康检查' : '添加健康检查', style: 'width:560px;max-width:95vw', segmented: true }, () =>
                h(NForm, { labelPlacement: 'left', labelWidth: 100 }, () => [
                    h(NFormItem, { label: '名称' }, () => h(NInput, { value: form.name, onUpdateValue: v => form.name = v, placeholder: '服务名称' })),
                    h(NFormItem, { label: 'URL' }, () => h(NInput, { value: form.url, onUpdateValue: v => form.url = v, placeholder: 'https://example.com/health' })),
                    h(NFormItem, { label: '方法' }, () => h(NSelect, { value: form.method, onUpdateValue: v => form.method = v, options: methodOptions, style: 'width:120px' })),
                    h(NFormItem, { label: '请求头' }, () => h(NInput, { type: 'textarea', value: form.headers_json, onUpdateValue: v => form.headers_json = v, placeholder: '{"Authorization": "Bearer token"}', rows: 2 })),
                    form.method !== 'GET' && form.method !== 'HEAD' ? h(NFormItem, { label: '请求体' }, () => h(NInput, { type: 'textarea', value: form.body, onUpdateValue: v => form.body = v, placeholder: '请求体内容', rows: 2 })) : null,
                    h(NFormItem, { label: '期望状态码' }, () => h(NInputNumber, { value: form.expected_status, onUpdateValue: v => form.expected_status = v, min: 100, max: 599, style: 'width:120px' })),
                    h(NFormItem, { label: '期望字段' }, () => h(NInput, { value: form.expected_field, onUpdateValue: v => form.expected_field = v, placeholder: '如: status (留空则只检查状态码)' })),
                    h(NFormItem, { label: '期望值' }, () => h(NInput, { value: form.expected_value, onUpdateValue: v => form.expected_value = v, placeholder: '如: UP, ok' })),
                    h(NFormItem, { label: '超时(秒)' }, () => h(NInputNumber, { value: form.timeout_sec, onUpdateValue: v => form.timeout_sec = v, min: 1, max: 300, style: 'width:120px' })),
                    h(NFormItem, { label: '间隔(秒)' }, () => h(NInputNumber, { value: form.interval_sec, onUpdateValue: v => form.interval_sec = v, min: 5, max: 3600, style: 'width:120px' })),
                    h('div', { style: 'display:flex;justify-content:flex-end;gap:8px;margin-top:8px' }, [
                        h(NButton, { onClick: () => showModal.value = false }, () => '取消'),
                        h(NButton, { type: 'primary', onClick: save }, () => '保存'),
                    ]),
                ])
            ),
        ]);
    }
});

// --- Health Check Logs ---
const HealthCheckLogsPage = defineComponent({
    setup() {
        const logs = ref([]);
        const total = ref(0);
        const page = ref(1);
        const pageSize = 20;
        const loading = ref(true);
        const { connected, messages, stop } = useWebSocket('/ws/healthcheck-logs');
        onUnmounted(stop);

        async function load() {
            loading.value = true;
            try {
                const res = await api.get('/api/health-checks/logs?page=' + page.value + '&page_size=' + pageSize);
                logs.value = res.data || [];
                total.value = res.total || 0;
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch(page, load);

        // Live updates
        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (latest && (latest.type === 'healthcheck_success' || latest.type === 'healthcheck_error') && page.value === 1) {
                load();
            }
        });

        const columns = useColumns([
            { title: '时间', key: 'detected_at', width: 150, render: row => h('span', { style: 'font-size:12px;opacity:0.65' }, formatTime(row.detected_at)) },
            { title: '服务', key: 'check_name', width: 120 },
            { title: '状态', key: 'status', width: 80, render: row => h(NTag, { type: row.status === 'up' ? 'success' : 'error', size: 'small', bordered: false }, () => row.status.toUpperCase()) },
            { title: 'HTTP', key: 'http_status', width: 70, _hideOnMobile: true },
            { title: '延迟', key: 'latency_ms', width: 80, render: row => row.latency_ms + 'ms' },
            { title: '错误', key: 'error', ellipsis: { tooltip: true }, _hideOnMobile: true },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px' }, [
                    h('h3', { class: 'page-title' }, '检查日志'),
                    h('div', { style: 'display:flex;align-items:center;gap:4px;font-size:12px;opacity:0.5' }, [
                        h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                        connected.value ? '实时' : '离线'
                    ]),
                ]),
            ]),
            h(NDataTable, { columns: columns.value, data: logs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 260px)', scrollX: _isMobile.value ? 400 : undefined }),
            total.value > pageSize ? h('div', { style: 'margin-top:16px;display:flex;justify-content:flex-end' },
                h(NPagination, { page: page.value, pageSize, itemCount: total.value, onUpdatePage: p => page.value = p })
            ) : null,
        ]);
    }
});

// ============================================================
// Layout
// ============================================================
const AppLayout = defineComponent({
    setup() {
        const route = VueRouter.useRoute();
        const router = VueRouter.useRouter();
        const user = ref(null);
        const siderCollapsed = ref(_isMobile.value);

        onMounted(async () => {
            try { user.value = await api.get('/api/auth/me'); } catch (e) {
                if (e.message !== 'network_error' && route.path !== '/login') router.push('/login');
            }
        });

        watch(_isMobile, (mobile) => { siderCollapsed.value = mobile; });

        const menuOptions = [
            { label: '仪表盘', key: 'dashboard' },
            { label: '健康检查', key: 'g-healthcheck' },
            { label: 'MySQL', key: 'g-mysql' },
            { label: 'RocketMQ', key: 'g-rocketmq' },
            { label: '监控日志', key: 'monitor-logs' },
            { label: '系统', key: 'g-system' },
        ];

        const groupTabs = {
            'g-mysql': [
                { label: '数据库', key: 'databases' },
                { label: '慢SQL', key: 'slow-queries' },
            ],
            'g-rocketmq': [
                { label: 'MQ 配置', key: 'rocketmq' },
                { label: 'MQ 告警', key: 'rocketmq-alerts' },
            ],
            'g-healthcheck': [
                { label: '检查配置', key: 'health-checks' },
                { label: '检查日志', key: 'health-checks-logs' },
            ],
            'g-system': [
                { label: '通知配置', key: 'notifications' },
                { label: '操作记录', key: 'audit-logs' },
                { label: '系统设置', key: 'settings' },
            ],
        };
        const routeToGroup = {};
        for (const [g, tabs] of Object.entries(groupTabs)) {
            for (const t of tabs) routeToGroup[t.key] = g;
        }

        const routeKey = computed(() => route.path.replace('/', '') || 'dashboard');
        const activeKey = computed(() => routeToGroup[routeKey.value] || routeKey.value);
        const currentTabs = computed(() => {
            const group = routeToGroup[routeKey.value];
            return group ? groupTabs[group] : null;
        });

        function handleMenuUpdate(key) {
            if (groupTabs[key]) {
                router.push('/' + groupTabs[key][0].key);
            } else {
                router.push('/' + key);
            }
            if (_isMobile.value) siderCollapsed.value = true;
        }

        async function logout() {
            try { await api.post('/api/auth/logout'); } catch {}
            router.push('/login');
        }

        return () => {
            if (route.path === '/login') return h(VueRouter.RouterView);

            // Mobile layout
            if (_isMobile.value) {
                return h(NLayout, { style: 'height:100vh' }, () => [
                    h('div', { class: 'topbar' }, [
                        h('div', { class: 'topbar-left' }, [
                            h(NButton, { quaternary: true, size: 'small', onClick: () => siderCollapsed.value = false, style: 'font-size:18px' }, () => '\u2630'),
                            h('span', { style: 'font-size:14px;font-weight:600' }, 'Ops Monitor'),
                        ]),
                        h('div', { class: 'topbar-right' }, [
                            h(NButton, { quaternary: true, circle: true, size: 'small', onClick: toggleTheme }, () => themeIcon()),
                            user.value ? h('div', { style: 'display:flex;align-items:center;gap:6px' }, [
                                user.value.avatar_url ? h(NAvatar, { src: user.value.avatar_url, size: 22, round: true }) : null,
                                h(NButton, { size: 'tiny', secondary: true, onClick: logout }, () => '退出'),
                            ]) : null,
                        ]),
                    ]),
                    h(NDrawer, { show: !siderCollapsed.value, 'onUpdate:show': v => { siderCollapsed.value = !v; }, placement: 'left', width: 220 }, () =>
                        h(NDrawerContent, { bodyContentStyle: 'padding:0' }, () => [
                            h('div', { class: 'sider-header' }, [
                                h('div', { class: 'sider-logo' }, 'O'),
                                h('span', { style: 'font-size:14px;font-weight:600' }, 'Ops Monitor'),
                            ]),
                            h(NMenu, { value: activeKey.value, options: menuOptions, onUpdateValue: handleMenuUpdate }),
                        ])
                    ),
                    h(NLayout, { contentStyle: 'padding:16px;overflow-y:auto' }, () => [
                        currentTabs.value ? h(NMenu, {
                            mode: 'horizontal',
                            value: routeKey.value,
                            options: currentTabs.value,
                            onUpdateValue: (key) => router.push('/' + key),
                            style: 'margin-bottom:12px',
                        }) : null,
                        h(VueRouter.RouterView),
                    ]),
                ]);
            }

            // Desktop layout: top bar → sidebar | sub-sidebar | content
            const topbarH = '48px';
            return h('div', { style: 'height:100vh;overflow:hidden' }, [
                // Full-width top bar: logo left, user info right
                h('div', { class: 'topbar' }, [
                    h('div', { class: 'topbar-left', style: 'cursor:pointer', onClick: () => router.push('/dashboard') }, [
                        h('div', { class: 'sider-logo' }, 'O'),
                        h('span', { class: 'sider-title' }, 'Ops Monitor'),
                    ]),
                    h('div', { class: 'topbar-right' }, [
                        h(NButton, { quaternary: true, circle: true, size: 'small', onClick: toggleTheme }, () => themeIcon()),
                        user.value ? h('div', { style: 'display:flex;align-items:center;gap:8px' }, [
                            user.value.avatar_url ? h(NAvatar, { src: user.value.avatar_url, size: 24, round: true }) : null,
                            h(NText, { depth: 2, style: 'font-size:12px' }, () => user.value.username || user.value.github_login || 'admin'),
                            h(NButton, { size: 'tiny', secondary: true, onClick: logout }, () => '退出'),
                        ]) : null,
                    ]),
                ]),
                // Below: sidebar | sub-sidebar | content — fills remaining height
                h(NLayout, { hasSider: true, style: `height:calc(100vh - ${topbarH});overflow:hidden` }, () => [
                    // Left sidebar (menu only)
                    h(NLayoutSider, { bordered: true, width: 180, nativeScrollbar: false }, () => [
                        h(NMenu, { value: activeKey.value, options: menuOptions, onUpdateValue: handleMenuUpdate }),
                    ]),
                    // Sub sidebar (when group has children)
                    currentTabs.value ? h(NLayoutSider, { bordered: true, width: 140, nativeScrollbar: false, contentStyle: 'padding:12px 0;background:var(--content-bg)' }, () => [
                        h(NMenu, {
                            value: routeKey.value,
                            options: currentTabs.value,
                            onUpdateValue: (key) => router.push('/' + key),
                        }),
                    ]) : null,
                    // Content
                    h(NLayout, { contentStyle: 'padding:28px 36px 48px;overflow-y:auto;background:var(--body-bg)' }, () => [
                        h(VueRouter.RouterView),
                    ]),
                ]),
            ]);
        };
    }
});

// ============================================================
// Router
// ============================================================
const routes = [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', component: LoginPage },
    { path: '/dashboard', component: DashboardPage },
    { path: '/databases', component: DatabasesPage },
    { path: '/notifications', component: NotificationsPage },
    { path: '/slow-queries', component: SlowQueriesPage },
    { path: '/monitor-logs', component: MonitorLogsPage },
    { path: '/rocketmq', component: RocketMQPage },
    { path: '/rocketmq-alerts', component: RocketMQAlertsPage },
    { path: '/health-checks', component: HealthChecksPage },
    { path: '/health-checks-logs', component: HealthCheckLogsPage },
    { path: '/audit-logs', component: AuditLogsPage },
    { path: '/settings', component: SettingsPage },
];

const router = createRouter({ history: createWebHashHistory(), routes });

router.beforeEach(async (to) => {
    if (to.path === '/login') { _sessionValid = false; return true; }
    if (_sessionValid) return true;
    try {
        await api.get('/api/auth/me');
        _sessionValid = true;
        return true;
    } catch (e) {
        // Network error — allow navigation, don't force login
        if (e.message === 'network_error') return true;
        return '/login';
    }
});

// ============================================================
// App
// ============================================================
// Expose message API globally for non-component usage
const MessageBridge = defineComponent({
    setup() {
        window.$message = useMessage();
        return () => null;
    }
});

const app = createApp({
    setup() {
        const currentTheme = computed(() => _isDark.value ? darkTheme : null);
        return () => h(NConfigProvider, { theme: currentTheme.value }, () =>
            h(NMessageProvider, { containerStyle: 'z-index:9999' }, () => [h(MessageBridge), h(AppLayout), SqlDetailModal()])
        );
    }
});

app.use(router);
app.mount('#app');
