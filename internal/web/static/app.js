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
    const ignoring = ref(false);
    async function handleIgnore() {
        if (!row || !row.database_id || !row.sql_text) return;
        ignoring.value = true;
        try {
            await api.post('/api/ignored-sql', { database_id: row.database_id, sql_text: row.sql_text });
            window.$message && window.$message.success('已忽略该SQL模式，后续相同SQL将不再通知');
            _sqlDetail.show = false;
        } catch (e) {
            window.$message && window.$message.error('忽略失败: ' + (e.message || e));
        }
        ignoring.value = false;
    }
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
                h('div', { style: 'display:flex;gap:8px' }, [
                    row && row.database_id ? h(NButton, { size: 'tiny', type: 'warning', secondary: true, loading: ignoring.value, onClick: handleIgnore }, () => '忽略此SQL') : null,
                    h(NButton, { size: 'tiny', secondary: true, onClick: () => { copyText(_sqlDetail.sql); } }, () => '复制'),
                ]),
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

        return () => h('div', { class: 'login-page' }, [
            // Theme toggle on login page
            h('div', { style: 'position:absolute;top:16px;right:16px;z-index:10' }, [
                h(NButton, { quaternary: true, circle: true, onClick: toggleTheme, style: 'font-size:18px' }, () => themeIcon()),
            ]),
            h('div', { class: 'login-card-wrapper' }, [
                h('div', { class: 'login-card' }, [
                    // Logo area
                    h('div', { class: 'login-header' }, [
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
                statCard('Grafana', '告警集成', [
                    { label: '运行中', value: stats.value.grafana_running || 0, color: '#18a058', link: '/grafana' },
                    { label: '已配置', value: stats.value.grafana_configs || 0, color: '#2080f0', link: '/grafana' },
                    { label: '今日告警', value: stats.value.grafana_alerts_today || 0, color: (stats.value.grafana_alerts_today || 0) > 0 ? '#d03050' : '#999', link: '/grafana-alerts' },
                ], '/grafana'),
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
            h(NDataTable, { columns: columns.value, data: databases.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 400 : undefined }),
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
        const scopes = ref({});
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const form = reactive({ type: 'feishu', scope_type: 'all', database_id: null, webhook: '', secret: '', smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', email_from: '', email_to: '', dootask_base_url: '', dootask_token: '', dootask_dialog_id: '' });
        const saving = ref(false);
        const message = useMessage();

        async function load() {
            loading.value = true;
            try {
                list.value = await api.get('/api/notifications');
                scopes.value = await api.get('/api/notification-scopes');
            } catch {}
            loading.value = false;
        }
        onMounted(load);

        const typeOptions = [
            { label: 'Lark', value: 'feishu' },
            { label: '钉钉', value: 'dingtalk' },
            { label: 'DooTask', value: 'dootask' },
            { label: '邮件', value: 'email' },
        ];
        const scopeTypeOptions = [
            { label: '全局（所有告警）', value: 'all' },
            { label: '健康检查', value: 'health' },
            { label: 'MySQL', value: 'mysql' },
            { label: '自定义SQL', value: 'custom_sql' },
            { label: 'RocketMQ', value: 'rocketmq' },
            { label: 'Grafana', value: 'grafana' },
        ];
        const scopeTypeLabels = { all: '全局', health: '健康检查', mysql: 'MySQL', custom_sql: '自定义SQL', rocketmq: 'RocketMQ', grafana: 'Grafana' };
        const scopeItemOptions = computed(() => {
            const items = scopes.value[form.scope_type] || [];
            return items.map(d => ({ label: d.name, value: d.id }));
        });

        function openAdd() {
            editingId.value = null;
            Object.assign(form, { type: 'feishu', scope_type: 'all', database_id: null, webhook: '', secret: '', smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', email_from: '', email_to: '', dootask_base_url: '', dootask_token: '', dootask_dialog_id: '' });
            showModal.value = true;
        }
        function fillFormFromRow(row) {
            form.type = row.type;
            form.scope_type = row.scope_type || 'all';
            form.database_id = row.database_id;
            const cfg = typeof row.config_json === 'string' ? JSON.parse(row.config_json) : row.config_json;
            if (row.type === 'dingtalk' || row.type === 'feishu') {
                form.webhook = cfg.webhook || '';
                form.secret = cfg.secret || '';
            } else if (row.type === 'dootask') {
                form.dootask_base_url = cfg.base_url || '';
                form.dootask_token = cfg.token || '';
                form.dootask_dialog_id = cfg.dialog_id || '';
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
                const payload = { ...form };
                if (payload.scope_type === 'all') payload.database_id = null;
                if (editingId.value) {
                    await api.put('/api/notifications/' + editingId.value, payload);
                    message.success('更新成功');
                } else {
                    await api.post('/api/notifications', payload);
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
            { title: '类型', key: 'type', width: 80, render: row => h(NTag, { type: 'info', size: 'small' }, () => ({ dingtalk: '钉钉', feishu: 'Lark', dootask: 'DooTask', email: '邮件' }[row.type] || row.type)) },
            { title: '关联配置', key: 'scope_name', render: row => {
                const label = scopeTypeLabels[row.scope_type] || '全局';
                if (row.scope_type === 'all') return h(NTag, { size: 'small' }, () => '全局');
                return h('span', [h(NTag, { size: 'small', type: 'info' }, () => label), row.scope_name ? h('span', { style: 'margin-left:4px;font-size:12px' }, row.scope_name) : null]);
            }, _hideOnMobile: true },
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
            h(NDataTable, { columns: columns.value, data: list.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 350 : undefined }),
            h(NModal, { show: showModal.value, 'onUpdate:show': v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑通知' : '添加通知', style: _isMobile.value ? 'width:95vw' : 'width:680px', segmented: true }, () => h(NForm, { model: form, labelPlacement: _isMobile.value ? 'top' : 'left', labelWidth: _isMobile.value ? undefined : 100 }, [
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '通知类型' }, () => h(NSelect, { value: form.type, 'onUpdate:value': v => form.type = v, options: typeOptions }))),
                    h(NGi, null, () => h(NFormItem, { label: '关联范围' }, () => h(NSelect, { value: form.scope_type, 'onUpdate:value': v => { form.scope_type = v; form.database_id = null; }, options: scopeTypeOptions }))),
                ]),
                form.scope_type !== 'all' ? h(NFormItem, { label: '关联配置' }, () => h(NSelect, { value: form.database_id, 'onUpdate:value': v => form.database_id = v, options: scopeItemOptions.value, clearable: true, placeholder: '选择具体配置（不选则该类型全部）' })) : null,
                (form.type === 'dingtalk' || form.type === 'feishu') ? h('div', [
                    h(NFormItem, { label: 'Webhook URL' }, () => h(NInput, { value: form.webhook, 'onUpdate:value': v => form.webhook = v, placeholder: 'https://...' })),
                    h(NFormItem, { label: '签名密钥' }, () => h(NInput, { value: form.secret, 'onUpdate:value': v => form.secret = v, placeholder: '可选' })),
                ]) : form.type === 'dootask' ? h('div', [
                    h(NFormItem, { label: '服务地址' }, () => h(NInput, { value: form.dootask_base_url, 'onUpdate:value': v => form.dootask_base_url = v, placeholder: 'https://t.hitosea.com' })),
                    h(NFormItem, { label: 'Token' }, () => h(NInput, { value: form.dootask_token, 'onUpdate:value': v => form.dootask_token = v, placeholder: '机器人Token' })),
                    h(NFormItem, { label: '对话ID' }, () => h(NInput, { value: form.dootask_dialog_id, 'onUpdate:value': v => form.dootask_dialog_id = v, placeholder: 'dialog_id' })),
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
        const clearing = ref(false);

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
        const selectedDBName = computed(() => {
            const item = databases.value.find(d => d.id === filterDB.value);
            return item ? item.name : '';
        });

        async function clearSlowLogs() {
            clearing.value = true;
            try {
                let url = '/api/slow-queries';
                if (filterDB.value) url += '?database_id=' + filterDB.value;
                const res = await api.del(url);
                window.$message && window.$message.success('已清空 ' + (res.deleted || 0) + ' 条慢SQL日志');
                page.value = 1;
                data.value = { logs: [], total: 0, page: 1, total_pages: 0 };
                await load();
            } catch (e) {
                window.$message && window.$message.error('清空失败: ' + (e.message || e));
            }
            clearing.value = false;
        }

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
                h('div', { style: _isMobile.value ? 'display:flex;gap:8px;width:100%' : 'display:flex;gap:8px;align-items:center' }, [
                    h(NPopconfirm, { onPositiveClick: clearSlowLogs }, {
                        trigger: () => h(NButton, { size: 'small', secondary: true, type: 'error', loading: clearing.value, disabled: loading.value || data.value.total === 0 }, () => '清空'),
                        default: () => filterDB.value ? '确定清空数据库「' + selectedDBName.value + '」的慢SQL日志？' : '确定清空全部慢SQL日志？'
                    }),
                    h(NSelect, { value: filterDB.value, 'onUpdate:value': v => { filterDB.value = v; page.value = 1; }, options: dbOptions.value, style: _isMobile.value ? 'flex:1;min-width:0' : 'width:180px', placeholder: '筛选数据库', clearable: true, size: 'small' }),
                ]),
            ]),
            h(NDataTable, { columns: columns.value, data: data.value.logs || [], bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 500 : undefined }),
            data.value.total_pages > 1 ? h('div', { style: 'margin-top:16px;display:flex;justify-content:center' }, [
                h(NPagination, { page: page.value, 'onUpdate:page': v => page.value = v, pageCount: data.value.total_pages, size: 'small' }),
            ]) : null,
        ]);
    }
});

// --- Ignored SQL Patterns ---
const IgnoredSQLPage = defineComponent({
    setup() {
        const data = ref([]);
        const databases = ref([]);
        const loading = ref(true);
        const filterDB = ref(null);

        async function load() {
            loading.value = true;
            try {
                let url = '/api/ignored-sql';
                if (filterDB.value) url += '?database_id=' + filterDB.value;
                data.value = await api.get(url);
                databases.value = await api.get('/api/databases-simple');
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch(filterDB, () => load());

        const dbOptions = computed(() => [
            { label: '全部', value: null },
            ...databases.value.map(d => ({ label: d.name, value: d.id }))
        ]);

        async function handleDelete(row) {
            try {
                await api.del('/api/ignored-sql/' + row.id);
                window.$message && window.$message.success('已取消忽略');
                load();
            } catch (e) {
                window.$message && window.$message.error('操作失败: ' + (e.message || e));
            }
        }

        const columns = useColumns([
            { title: '数据库', key: 'database_name', width: 120 },
            { title: 'SQL 指纹', key: 'fingerprint', ellipsis: { tooltip: true }, render: row => h('code', { style: 'font-family:var(--font-mono);font-size:11px;opacity:0.7;cursor:pointer;text-decoration:underline dotted;text-underline-offset:3px', onClick: () => showSqlDetail({ sql_text: row.fingerprint, database_name: row.database_name }) }, truncate(row.fingerprint, _isMobile.value ? 40 : 80)) },
            { title: '样例SQL', key: 'sample_sql', ellipsis: { tooltip: true }, _hideOnMobile: true, render: row => h('code', { style: 'font-family:var(--font-mono);font-size:11px;opacity:0.5;cursor:pointer;text-decoration:underline dotted;text-underline-offset:3px', onClick: () => showSqlDetail({ sql_text: row.sample_sql, database_name: row.database_name }) }, truncate(row.sample_sql, 60)) },
            { title: '添加时间', key: 'created_at', width: 140, _hideOnMobile: true, render: row => formatTime(row.created_at) },
            { title: '操作', key: 'actions', width: 80, render: row => h(NButton, { size: 'tiny', type: 'error', secondary: true, onClick: () => handleDelete(row) }, () => '取消忽略') },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: _isMobile.value ? 'margin-bottom:12px' : 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px;margin-bottom:' + (_isMobile.value ? '8px' : '0') }, [
                    h('h3', { class: 'page-title' }, '已忽略的SQL'),
                    h(NText, { depth: 3 }, () => '共 ' + (data.value || []).length + ' 条'),
                ]),
                h(NSelect, { value: filterDB.value, 'onUpdate:value': v => { filterDB.value = v; }, options: dbOptions.value, style: _isMobile.value ? 'width:100%' : 'width:180px', placeholder: '筛选数据库', clearable: true, size: 'small' }),
            ]),
            h(NDataTable, { columns: columns.value, data: data.value || [], bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 500 : undefined }),
        ]);
    }
});

// --- Custom SQL Checks ---
const CustomSQLPage = defineComponent({
    setup() {
        const checks = ref([]);
        const databases = ref([]);
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const saving = ref(false);
        const importing = ref(false);
        const importInputRef = ref(null);
        const form = reactive({
            database_id: null,
            name: '',
            db_name: '',
            sql_text: '',
            result_field: '',
            interval_sec: 30,
            timeout_sec: 10,
            alert_strategy: 'threshold',
            condition: 'gt',
            expected_value: '0',
            alert_delta_value: '',
            alert_delta_percent: '',
            alert_consecutive: 1,
            alert_rules: [],
            notify_enabled: true,
            recovery_notify: true,
            message_template: '',
        });
        const message = useMessage();

        const conditionOptions = [
            { label: '> 数值大于', value: 'gt' },
            { label: '>= 数值大于等于', value: 'gte' },
            { label: '< 数值小于', value: 'lt' },
            { label: '<= 数值小于等于', value: 'lte' },
            { label: '== 等于', value: 'eq' },
            { label: '!= 不等于', value: 'ne' },
            { label: '包含文本', value: 'contains' },
            { label: '不包含文本', value: 'not_contains' },
            { label: '为空', value: 'empty' },
            { label: '不为空', value: 'not_empty' },
            { label: '发生变化', value: 'changed' },
            { label: '每次都上报', value: 'always' },
        ];
        const conditionLabels = Object.fromEntries(conditionOptions.map(o => [o.value, o.label]));
        const strategyOptions = [
            { label: '单次阈值', value: 'threshold' },
            { label: '连续命中阈值', value: 'sustained' },
            { label: '突增', value: 'increase' },
            { label: '连续上升', value: 'continuous_increase' },
        ];
        const strategyLabels = Object.fromEntries(strategyOptions.map(o => [o.value, o.label]));
        const ruleNeedsExpected = rule => !['empty', 'not_empty', 'changed', 'always'].includes(rule.condition);
        const ruleUsesDelta = rule => rule.alert_strategy === 'increase';
        const ruleUsesConsecutive = rule => ['sustained', 'continuous_increase'].includes(rule.alert_strategy);

        async function load() {
            loading.value = true;
            try {
                checks.value = await api.get('/api/custom-sql');
                databases.value = await api.get('/api/databases-simple');
            } catch {}
            loading.value = false;
        }
        onMounted(load);

        const dbOptions = computed(() => databases.value.map(d => ({ label: d.name, value: d.id })));
        const gridCols = computed(() => _isMobile.value ? 1 : 2);

        function resetForm() {
            Object.assign(form, {
                database_id: databases.value[0] ? databases.value[0].id : null,
                name: '',
                db_name: '',
                sql_text: 'SELECT COUNT(*) AS process_count FROM information_schema.PROCESSLIST',
                result_field: '',
                interval_sec: 30,
                timeout_sec: 10,
                alert_strategy: 'threshold',
                condition: 'gt',
                expected_value: '0',
                alert_delta_value: '',
                alert_delta_percent: '',
                alert_consecutive: 1,
                alert_rules: [],
                notify_enabled: true,
                recovery_notify: true,
                message_template: '',
            });
        }
        function openAdd() {
            editingId.value = null;
            resetForm();
            showModal.value = true;
        }
        function openEdit(row) {
            editingId.value = row.id;
            const rules = parseCustomSQLAlertRules(row.alert_rules, row);
            Object.assign(form, {
                database_id: row.database_id,
                name: row.name,
                db_name: row.db_name || '',
                sql_text: row.sql_text,
                result_field: row.result_field || '',
                interval_sec: row.interval_sec,
                timeout_sec: row.timeout_sec,
                alert_strategy: row.alert_strategy || 'threshold',
                condition: row.condition || 'gt',
                expected_value: row.expected_value || '',
                alert_delta_value: row.alert_delta_value || '',
                alert_delta_percent: row.alert_delta_percent || '',
                alert_consecutive: row.alert_consecutive || 1,
                alert_rules: rules,
                notify_enabled: row.notify_enabled !== false,
                recovery_notify: row.recovery_notify !== false,
                message_template: row.message_template || '',
            });
            showModal.value = true;
        }
        function openClone(row) {
            editingId.value = null;
            openEdit(row);
            editingId.value = null;
            form.name = row.name + ' (副本)';
        }
        async function save() {
            saving.value = true;
            try {
                const payload = customSQLPayload();
                if (editingId.value) {
                    await api.put('/api/custom-sql/' + editingId.value, payload);
                    message.success('更新成功');
                } else {
                    await api.post('/api/custom-sql', payload);
                    message.success('创建成功');
                }
                showModal.value = false;
                await load();
            } catch (e) {
                message.error(e.message);
            }
            saving.value = false;
        }
        function normalizeCustomSQLAlertRule(rule = {}) {
            const strategy = rule.alert_strategy || rule.strategy || 'threshold';
            const expectedValue = rule.expected_value || rule.value || rule.alert_value || '';
            const deltaValue = rule.alert_delta_value || rule.delta_value || '';
            const deltaPercent = rule.alert_delta_percent || rule.delta_percent || '';
            const consecutive = rule.alert_consecutive || rule.consecutive || 1;
            return {
                name: rule.name || '',
                result_field: rule.result_field || rule.field || '',
                alert_strategy: strategy,
                strategy,
                condition: rule.condition || rule.alert_condition || 'gt',
                expected_value: expectedValue,
                value: expectedValue,
                alert_delta_value: deltaValue,
                delta_value: deltaValue,
                alert_delta_percent: deltaPercent,
                delta_percent: deltaPercent,
                alert_consecutive: consecutive,
                consecutive,
            };
        }
        function parseCustomSQLAlertRules(raw, row) {
            let parsed = [];
            if (Array.isArray(raw)) parsed = raw;
            else {
                try {
                    const value = JSON.parse(raw || '[]');
                    parsed = Array.isArray(value) ? value : [];
                } catch { parsed = []; }
            }
            parsed = parsed.map(normalizeCustomSQLAlertRule);
            if (parsed.length === 0 && row) {
                parsed.push(normalizeCustomSQLAlertRule({
                    name: row.result_field || '第一列',
                    result_field: row.result_field || '',
                    alert_strategy: row.alert_strategy || 'threshold',
                    condition: row.condition || 'gt',
                    expected_value: row.expected_value || '',
                    alert_delta_value: row.alert_delta_value || '',
                    alert_delta_percent: row.alert_delta_percent || '',
                    alert_consecutive: row.alert_consecutive || 1,
                }));
            }
            return parsed;
        }
        function customSQLPayload() {
            const rules = (form.alert_rules || []).map(normalizeCustomSQLAlertRule);
            const firstRule = rules[0] || normalizeCustomSQLAlertRule({
                result_field: form.result_field,
                alert_strategy: form.alert_strategy,
                condition: form.condition,
                expected_value: form.expected_value,
                alert_delta_value: form.alert_delta_value,
                alert_delta_percent: form.alert_delta_percent,
                alert_consecutive: form.alert_consecutive,
            });
            return {
                ...form,
                alert_rules: JSON.stringify(rules),
                result_field: firstRule.result_field || '',
                alert_strategy: firstRule.alert_strategy || 'threshold',
                condition: firstRule.condition || 'gt',
                expected_value: firstRule.expected_value || '',
                alert_delta_value: firstRule.alert_delta_value || '',
                alert_delta_percent: firstRule.alert_delta_percent || '',
                alert_consecutive: firstRule.alert_consecutive || 1,
                notify_enabled: !!form.notify_enabled,
                recovery_notify: !!form.recovery_notify,
                message_template: form.message_template || '',
            };
        }
        function customSQLExportItem(row) {
            return {
                database_name: row.database_name || '',
                database_id: row.database_id || null,
                name: row.name || '',
                db_name: row.db_name || '',
                sql_text: row.sql_text || '',
                result_field: row.result_field || '',
                interval_sec: row.interval_sec || 30,
                timeout_sec: row.timeout_sec || 10,
                alert_strategy: row.alert_strategy || 'threshold',
                condition: row.condition || 'gt',
                expected_value: row.expected_value || '',
                alert_delta_value: row.alert_delta_value || '',
                alert_delta_percent: row.alert_delta_percent || '',
                alert_consecutive: row.alert_consecutive || 1,
                alert_rules: row.alert_rules || '[]',
                notify_enabled: row.notify_enabled !== false,
                recovery_notify: row.recovery_notify !== false,
                message_template: row.message_template || '',
                enabled: row.enabled !== false,
            };
        }
        function exportCustomSQLChecks() {
            if (!checks.value.length) {
                message.warning('没有可导出的自定义 SQL 监控');
                return;
            }
            const payload = {
                type: 'ops-sentinel.custom_sql_checks',
                version: 1,
                exported_at: new Date().toISOString(),
                items: checks.value.map(customSQLExportItem),
            };
            const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'ops-sentinel-custom-sql-' + new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-') + '.json';
            document.body.appendChild(a);
            a.click();
            a.remove();
            URL.revokeObjectURL(url);
            message.success('已导出 ' + checks.value.length + ' 条配置');
        }
        function triggerImportCustomSQL() {
            if (importInputRef.value) importInputRef.value.click();
        }
        function parseCustomSQLImportItems(raw) {
            const parsed = JSON.parse(raw);
            if (Array.isArray(parsed)) return parsed;
            if (Array.isArray(parsed.items)) return parsed.items;
            if (Array.isArray(parsed.custom_sql_checks)) return parsed.custom_sql_checks;
            throw new Error('导入文件格式不正确');
        }
        function resolveImportDatabaseID(item) {
            const dbs = databases.value || [];
            const byName = item.database_name ? dbs.find(d => d.name === item.database_name) : null;
            if (byName) return byName.id;
            const byID = item.database_id ? dbs.find(d => d.id === item.database_id) : null;
            if (byID) return byID.id;
            if (dbs.length === 1) return dbs[0].id;
            throw new Error('找不到数据库连接: ' + (item.database_name || item.database_id || item.name || '未指定'));
        }
        function importCustomSQLPayload(item) {
            let alertRules = item.alert_rules;
            if (Array.isArray(alertRules)) alertRules = JSON.stringify(alertRules.map(normalizeCustomSQLAlertRule));
            if (typeof alertRules !== 'string') alertRules = '[]';
            const rules = parseCustomSQLAlertRules(alertRules, item);
            const firstRule = rules[0] || normalizeCustomSQLAlertRule(item);
            return {
                database_id: resolveImportDatabaseID(item),
                name: item.name || '导入的自定义SQL',
                db_name: item.db_name || '',
                sql_text: item.sql_text || '',
                result_field: item.result_field || firstRule.result_field || '',
                interval_sec: item.interval_sec || 30,
                timeout_sec: item.timeout_sec || 10,
                alert_strategy: item.alert_strategy || firstRule.alert_strategy || 'threshold',
                condition: item.condition || firstRule.condition || 'gt',
                expected_value: item.expected_value || firstRule.expected_value || '',
                alert_delta_value: item.alert_delta_value || firstRule.alert_delta_value || '',
                alert_delta_percent: item.alert_delta_percent || firstRule.alert_delta_percent || '',
                alert_consecutive: item.alert_consecutive || firstRule.alert_consecutive || 1,
                alert_rules: JSON.stringify(rules),
                notify_enabled: item.notify_enabled !== false,
                recovery_notify: item.recovery_notify !== false,
                message_template: item.message_template || '',
            };
        }
        async function importCustomSQLFile(event) {
            const file = event.target.files && event.target.files[0];
            event.target.value = '';
            if (!file) return;
            importing.value = true;
            try {
                const text = await file.text();
                const items = parseCustomSQLImportItems(text);
                if (!items.length) {
                    message.warning('导入文件里没有配置');
                    return;
                }
                let ok = 0;
                const failures = [];
                for (const item of items) {
                    try {
                        const payload = importCustomSQLPayload(item);
                        const created = await api.post('/api/custom-sql', payload);
                        if (item.enabled === false && created && created.id) {
                            await api.post('/api/custom-sql/' + created.id + '/toggle');
                        }
                        ok++;
                    } catch (e) {
                        failures.push((item && item.name ? item.name : '未命名配置') + ': ' + (e.message || e));
                    }
                }
                await load();
                if (failures.length) {
                    message.warning('导入完成：成功 ' + ok + ' 条，失败 ' + failures.length + ' 条；第一条失败：' + failures[0]);
                } else {
                    message.success('导入成功：' + ok + ' 条');
                }
            } catch (e) {
                message.error(e.message || '导入失败');
            } finally {
                importing.value = false;
            }
        }
        function addCustomSQLAlertRule(rule = {}) {
            form.alert_rules.push(normalizeCustomSQLAlertRule(rule));
        }
        function removeCustomSQLAlertRule(index) {
            form.alert_rules.splice(index, 1);
        }
        function formatCustomSQLRule(rule) {
            const normalized = normalizeCustomSQLAlertRule(rule);
            const field = normalized.result_field || '第一列';
            const strategy = strategyLabels[normalized.alert_strategy] || normalized.alert_strategy;
            let text = field + ' / ' + strategy;
            if (normalized.alert_strategy === 'increase') {
                const parts = [];
                if (normalized.alert_delta_value) parts.push('变化量>=' + normalized.alert_delta_value);
                if (normalized.alert_delta_percent) parts.push('变化率>=' + normalized.alert_delta_percent + '%');
                if (normalized.expected_value) parts.push((conditionLabels[normalized.condition] || normalized.condition) + ' ' + normalized.expected_value);
                return text + ' / ' + (parts.join('，') || '比上次上升');
            }
            if (normalized.alert_strategy === 'continuous_increase') {
                text += ' / 连续 ' + (normalized.alert_consecutive || 1) + ' 次';
                if (normalized.expected_value) text += ' / ' + (conditionLabels[normalized.condition] || normalized.condition) + ' ' + normalized.expected_value;
                return text;
            }
            text += ' / ' + (conditionLabels[normalized.condition] || normalized.condition);
            if (normalized.expected_value) text += ' ' + normalized.expected_value;
            if (normalized.alert_strategy === 'sustained') text += ' / 连续 ' + (normalized.alert_consecutive || 1) + ' 次';
            return text;
        }
        async function toggle(row) {
            try { await api.post('/api/custom-sql/' + row.id + '/toggle'); await load(); } catch (e) { message.error(e.message); }
        }
        async function del(row) {
            try { await api.del('/api/custom-sql/' + row.id); message.success('已删除'); await load(); } catch (e) { message.error(e.message); }
        }
        async function test(row) {
            try {
                const res = await api.post('/api/custom-sql/' + row.id + '/test');
                if (res.status === 'error') message.error(res.error || res.message);
                else message.success('当前值: ' + (res.value || '') + '，状态: ' + res.status);
            } catch (e) { message.error(e.message); }
        }

        const columns = useColumns([
            { title: '名称', key: 'name', width: 180, ellipsis: { tooltip: true }, render: row => h(NText, { strong: true, style: 'display:block;white-space:normal;word-break:break-word;line-height:1.35' }, () => row.name) },
            { title: '数据库', key: 'database_name', width: 120, _hideOnMobile: true },
            { title: '执行库', key: 'db_name', width: 150, _hideOnMobile: true, ellipsis: { tooltip: true }, render: row => row.db_name || 'performance_schema' },
            { title: '规则', key: 'alert_rules', width: 280, _hideOnMobile: true, render: row => {
                const rules = parseCustomSQLAlertRules(row.alert_rules, row);
                if (rules.length > 1) return h(NText, { depth: 3, style: 'font-size:12px;line-height:1.35;white-space:normal' }, () => rules.length + ' 条: ' + rules.map(r => r.name || r.result_field || '第一列').join(' / '));
                return h(NText, { depth: 3, style: 'font-size:12px;line-height:1.35;white-space:normal' }, () => formatCustomSQLRule(rules[0] || row));
            } },
            { title: 'SQL', key: 'sql_text', width: 360, ellipsis: { tooltip: true }, render: row => h('code', { style: 'font-family:var(--font-mono);font-size:11px;opacity:0.7;cursor:pointer;text-decoration:underline dotted;text-underline-offset:3px;white-space:nowrap', onClick: () => showSqlDetail({ sql_text: row.sql_text, database_name: row.database_name }) }, truncate(row.sql_text, _isMobile.value ? 36 : 90)) },
            { title: '状态', key: 'status', width: 90, render: row => row.running ? h(NTag, { type: 'success', size: 'small' }, () => '运行中') : row.enabled ? h(NTag, { type: 'warning', size: 'small' }, () => '已启用') : h(NTag, { size: 'small' }, () => '已禁用') },
            { title: '操作', key: 'actions', width: _isMobile.value ? 160 : 330, fixed: _isMobile.value ? undefined : 'right', render: row => h(NSpace, { size: 'small', wrap: false }, () => [
                h(NButton, { size: 'small', secondary: true, onClick: () => toggle(row) }, () => row.enabled ? '禁用' : '启用'),
                h(NButton, { size: 'small', secondary: true, onClick: () => openEdit(row) }, () => '编辑'),
                !_isMobile.value ? h(NButton, { size: 'small', secondary: true, onClick: () => openClone(row) }, () => '复制') : null,
                !_isMobile.value ? h(NButton, { size: 'small', secondary: true, onClick: () => test(row) }, () => '测试') : null,
                h(NPopconfirm, { onPositiveClick: () => del(row) }, { trigger: () => h(NButton, { size: 'small', secondary: true, type: 'error' }, () => '删除'), default: () => '确定删除？' }),
            ].filter(Boolean)) },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, '自定义SQL监控'),
                h('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap;justify-content:flex-end' }, [
                    h('input', { ref: importInputRef, type: 'file', accept: '.json,application/json', style: 'display:none', onChange: importCustomSQLFile }),
                    h(NButton, { secondary: true, onClick: exportCustomSQLChecks, size: _isMobile.value ? 'small' : 'medium' }, () => '导出'),
                    h(NButton, { secondary: true, loading: importing.value, onClick: triggerImportCustomSQL, size: _isMobile.value ? 'small' : 'medium' }, () => '导入'),
                    h(NButton, { type: 'primary', onClick: openAdd, size: _isMobile.value ? 'small' : 'medium' }, () => '+ 添加'),
                ]),
            ]),
            h(NDataTable, { columns: columns.value, data: checks.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 620 : 1590 }),
            h(NModal, { show: showModal.value, 'onUpdate:show': v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑自定义SQL' : '添加自定义SQL', style: _isMobile.value ? 'width:95vw' : 'width:1120px;max-width:96vw', segmented: true }, () => h(NForm, { model: form, labelPlacement: _isMobile.value ? 'top' : 'left', labelWidth: _isMobile.value ? undefined : 120 }, [
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '名称' }, () => h(NInput, { value: form.name, 'onUpdate:value': v => form.name = v, placeholder: '如: 待处理订单数量' }))),
                    h(NGi, null, () => h(NFormItem, { label: '数据库连接' }, () => h(NSelect, { value: form.database_id, 'onUpdate:value': v => form.database_id = v, options: dbOptions.value, placeholder: '选择数据库' }))),
                ]),
                h(NFormItem, { label: '执行库' }, () => h(NInput, { value: form.db_name, 'onUpdate:value': v => form.db_name = v, placeholder: '可选，不填默认 performance_schema；也可以在SQL里写库名.表名' })),
                h(NFormItem, { label: 'SQL' }, () => h('div', { style: 'width:100%;display:flex;flex-direction:column;gap:6px' }, [
                    h(NInput, { value: form.sql_text, 'onUpdate:value': v => form.sql_text = v, type: 'textarea', autosize: { minRows: 4, maxRows: 10 }, placeholder: 'SELECT COUNT(*) AS process_count FROM information_schema.PROCESSLIST' }),
                    h(NText, { depth: 3, style: 'font-size:12px;line-height:1.45' }, () => '只允许查询 SQL。禁止写入/锁表/SLEEP/SELECT *；普通 SELECT 必须带 LIMIT，聚合单行查询如 COUNT/SUM 可不带 LIMIT。'),
                ])),
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '检查间隔(秒)' }, () => h(NInputNumber, { value: form.interval_sec, 'onUpdate:value': v => form.interval_sec = v, min: 1 }))),
                    h(NGi, null, () => h(NFormItem, { label: '超时(秒)' }, () => h(NInputNumber, { value: form.timeout_sec, 'onUpdate:value': v => form.timeout_sec = v, min: 1 }))),
                ]),
                h(NFormItem, { label: '结果规则' }, () => h('div', { style: 'width:100%;display:flex;flex-direction:column;gap:10px' }, [
                    h('div', { style: 'display:flex;align-items:center;gap:8px;flex-wrap:wrap' }, [
                        h(NButton, { size: 'small', secondary: true, onClick: () => addCustomSQLAlertRule({ name: '新规则', alert_strategy: 'threshold', condition: 'gt', expected_value: '0' }) }, () => '+ 规则'),
                        h(NText, { depth: 3, style: 'font-size:12px' }, () => 'SQL 返回多列时，可为不同列分别配置告警规则；任意一条命中就会上报。'),
                    ]),
                    (form.alert_rules || []).length === 0 ? h(NText, { depth: 3, style: 'font-size:12px' }, () => '未配置时会按第一列和旧的单规则字段判断。') : null,
                    ...(form.alert_rules || []).map((rule, index) => h('div', { style: 'border:1px solid rgba(128,128,128,.22);border-radius:6px;padding:10px;display:flex;flex-direction:column;gap:8px' }, [
                        h(NGrid, { cols: _isMobile.value ? 1 : 2, xGap: 8, yGap: 8 }, () => [
                            h(NGi, null, () => h(NInput, { value: rule.name, 'onUpdate:value': v => rule.name = v, placeholder: '规则名称，如 连接数异常' })),
                            h(NGi, null, () => h(NInput, { value: rule.result_field, 'onUpdate:value': v => rule.result_field = v, placeholder: '结果字段：列名或序号；不填取第一列' })),
                            h(NGi, null, () => h(NSelect, { value: rule.alert_strategy, 'onUpdate:value': v => rule.alert_strategy = v, options: strategyOptions })),
                            h(NGi, null, () => h(NSelect, { value: rule.condition, 'onUpdate:value': v => rule.condition = v, options: conditionOptions })),
                            ruleNeedsExpected(rule) ? h(NGi, null, () => h(NInput, { value: rule.expected_value, 'onUpdate:value': v => rule.expected_value = v, placeholder: '期望值/阈值，如 3' })) : null,
                            ruleUsesDelta(rule) ? h(NGi, null, () => h(NInput, { value: rule.alert_delta_value, 'onUpdate:value': v => rule.alert_delta_value = v, placeholder: '变化量>=，如 500' })) : null,
                            ruleUsesDelta(rule) ? h(NGi, null, () => h(NInput, { value: rule.alert_delta_percent, 'onUpdate:value': v => rule.alert_delta_percent = v, placeholder: '变化率>=，如 30' })) : null,
                            ruleUsesConsecutive(rule) ? h(NGi, null, () => h(NInputNumber, { value: rule.alert_consecutive, 'onUpdate:value': v => rule.alert_consecutive = v, min: 1, max: 100, style: 'width:100%', placeholder: '连续次数' })) : null,
                        ]),
                        h('div', { style: 'display:flex;justify-content:space-between;align-items:center;gap:8px' }, [
                            h(NText, { depth: 3, style: 'font-size:12px;line-height:1.4' }, () => {
                                if (rule.alert_strategy === 'increase') return '突增：与上一次采样比较，变化量和变化率任一满足即可。';
                                if (rule.alert_strategy === 'continuous_increase') return '连续上升：连续多次比上一次采样更高，可叠加当前值阈值。';
                                if (rule.alert_strategy === 'sustained') return '连续命中阈值：连续多次满足条件才告警。';
                                return '单次阈值：本次满足条件即告警。';
                            }),
                            h(NButton, { size: 'tiny', secondary: true, type: 'error', onClick: () => removeCustomSQLAlertRule(index) }, () => '删除'),
                        ]),
                    ])),
                ])),
                h(NFormItem, { label: '通知' }, () => h(NGrid, { cols: _isMobile.value ? 1 : 2, xGap: 12, yGap: 8, style: 'width:100%' }, () => [
                    h(NGi, null, () => h('div', { style: 'display:flex;align-items:center;justify-content:space-between;gap:12px;border:1px solid rgba(128,128,128,.18);border-radius:6px;padding:10px 12px' }, [
                        h('div', null, [
                            h(NText, null, () => '命中后通知'),
                            h(NText, { depth: 3, style: 'display:block;font-size:12px;line-height:1.35;margin-top:2px' }, () => '异常首次命中时发送，未恢复前不重复刷屏。'),
                        ]),
                        h(NSwitch, { value: form.notify_enabled, 'onUpdate:value': v => form.notify_enabled = v }),
                    ])),
                    h(NGi, null, () => h('div', { style: 'display:flex;align-items:center;justify-content:space-between;gap:12px;border:1px solid rgba(128,128,128,.18);border-radius:6px;padding:10px 12px' }, [
                        h('div', null, [
                            h(NText, null, () => '恢复通知'),
                            h(NText, { depth: 3, style: 'display:block;font-size:12px;line-height:1.35;margin-top:2px' }, () => '告警恢复正常后发送恢复消息。'),
                        ]),
                        h(NSwitch, { value: form.recovery_notify, disabled: !form.notify_enabled, 'onUpdate:value': v => form.recovery_notify = v }),
                    ])),
                ])),
                h(NButton, { type: 'primary', block: true, loading: saving.value, onClick: save, style: 'margin-top:8px' }, () => editingId.value ? '保存' : '创建'),
            ])),
        ]);
    }
});

// --- Custom SQL Logs ---
const CustomSQLLogsPage = defineComponent({
    setup() {
        const data = ref({ data: [], total: 0, page: 1, total_pages: 0 });
        const checks = ref([]);
        const loading = ref(true);
        const filterCheck = ref(null);
        const page = ref(1);
        const clearing = ref(false);
        const message = useMessage();

        const { connected, messages, stop } = useWebSocket('/ws/custom-sql-logs');
        onUnmounted(stop);

        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (latest && latest.type === 'custom_sql_result' && latest.data) {
                if (!latest.data.detected_at || isInvalidTime(latest.data.detected_at)) {
                    latest.data.detected_at = new Date().toISOString();
                }
                if (!filterCheck.value || latest.database_id === filterCheck.value) {
                    data.value.data.unshift(latest.data);
                    if (data.value.data.length > 200) data.value.data.length = 200;
                    data.value.total++;
                }
            }
            if (messages.value.length > 500) messages.value.splice(0, messages.value.length - 500);
        });

        async function load() {
            loading.value = true;
            try {
                let url = '/api/custom-sql/logs?page=' + page.value;
                if (filterCheck.value) url += '&check_id=' + filterCheck.value;
                data.value = await api.get(url);
                checks.value = await api.get('/api/custom-sql');
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch([page, filterCheck], () => load());

        const checkOptions = computed(() => [
            { label: '全部', value: null },
            ...checks.value.map(c => ({ label: c.name, value: c.id }))
        ]);
        const selectedCheckName = computed(() => {
            const item = checks.value.find(c => c.id === filterCheck.value);
            return item ? item.name : '';
        });

        async function clearResultLogs() {
            clearing.value = true;
            try {
                let url = '/api/custom-sql/logs';
                if (filterCheck.value) url += '?check_id=' + filterCheck.value;
                const res = await api.del(url);
                message.success('已清空 ' + (res.deleted || 0) + ' 条结果日志');
                page.value = 1;
                data.value = { data: [], total: 0, page: 1, total_pages: 0 };
                await load();
            } catch (e) {
                message.error(e.message);
            }
            clearing.value = false;
        }

        const columns = useColumns([
            { title: '检测时间', key: 'detected_at', width: 140, render: row => h(NText, { depth: 3, style: 'font-size:12px' }, () => formatTime(row.detected_at)) },
            { title: '规则', key: 'check_name', width: 140 },
            { title: '数据库', key: 'database_name', width: 110, _hideOnMobile: true },
            { title: '状态', key: 'status', width: 80, render: row => row.status === 'alert' ? h(NTag, { type: 'error', size: 'small' }, () => '告警') : row.status === 'error' ? h(NTag, { type: 'warning', size: 'small' }, () => '错误') : h(NTag, { type: 'success', size: 'small' }, () => '正常') },
            { title: '当前值', key: 'value', width: 120, render: row => h('code', { style: 'font-family:var(--font-mono);font-size:12px' }, row.value || '') },
            { title: '条件', key: 'condition', width: 130, _hideOnMobile: true, render: row => (row.condition || '') + (row.expected_value ? ' ' + row.expected_value : '') },
            { title: '结果', key: 'message', ellipsis: { tooltip: true }, render: row => row.error || row.message },
            { title: '耗时', key: 'duration_ms', width: 80, _hideOnMobile: true, render: row => row.duration_ms + 'ms' },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: _isMobile.value ? 'margin-bottom:12px' : 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px;margin-bottom:' + (_isMobile.value ? '8px' : '0') }, [
                    h('h3', { class: 'page-title' }, 'SQL结果日志'),
                    h(NText, { depth: 3 }, () => '共 ' + data.value.total + ' 条'),
                    h('div', { style: 'display:flex;align-items:center;gap:4px;font-size:12px;opacity:0.5' }, [
                        h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                        connected.value ? '实时' : '断开'
                    ]),
                ]),
                h('div', { style: _isMobile.value ? 'display:flex;gap:8px;width:100%' : 'display:flex;gap:8px;align-items:center' }, [
                    h(NPopconfirm, { onPositiveClick: clearResultLogs }, {
                        trigger: () => h(NButton, { size: 'small', secondary: true, type: 'error', loading: clearing.value, disabled: loading.value || data.value.total === 0 }, () => '清空'),
                        default: () => filterCheck.value ? '确定清空规则「' + selectedCheckName.value + '」的结果日志？' : '确定清空全部 SQL 结果日志？'
                    }),
                    h(NSelect, { value: filterCheck.value, 'onUpdate:value': v => { filterCheck.value = v; page.value = 1; }, options: checkOptions.value, style: _isMobile.value ? 'flex:1;min-width:0' : 'width:220px', placeholder: '筛选规则', clearable: true, size: 'small' }),
                ]),
            ]),
            h(NDataTable, { columns: columns.value, data: data.value.data || [], bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 620 : undefined }),
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
        const settings = reactive({ github_client_id: '', github_client_secret: '', github_enabled: '0', password_login_enabled: '1', oauth_public_base_url: '' });
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
                    oauth_public_base_url: settings.oauth_public_base_url,
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
                h(NFormItem, { label: 'OAuth 公网根地址' }, () => h(NInput, { value: settings.oauth_public_base_url, 'onUpdate:value': v => settings.oauth_public_base_url = v, placeholder: 'https://你的域名（无末尾/），Docker/反代后必填' })),
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
    if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return '';
    return (d.getMonth()+1).toString().padStart(2,'0') + '-' +
           d.getDate().toString().padStart(2,'0') + ' ' +
           d.getHours().toString().padStart(2,'0') + ':' +
           d.getMinutes().toString().padStart(2,'0') + ':' +
           d.getSeconds().toString().padStart(2,'0');
}

function formatTimeShort(t) {
    if (!t) return '';
    const d = new Date(t);
    if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return '';
    return d.getHours().toString().padStart(2,'0') + ':' +
           d.getMinutes().toString().padStart(2,'0') + ':' +
           d.getSeconds().toString().padStart(2,'0');
}

function isInvalidTime(t) {
    const d = new Date(t);
    return Number.isNaN(d.getTime()) || d.getFullYear() < 2000;
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
        const form = reactive({ name: '', dashboard_url: '', username: '', password: '', consumer_group: '', topic: '', threshold: 1000, interval_sec: 30, notify_new_msg: false });
        const saving = ref(false);
        const groupOptions = ref([]);
        const topicOptions = ref([]);
        const groupLoading = ref(false);
        const topicLoading = ref(false);
        const message = useMessage();

        async function fetchGroups() {
            if (!form.dashboard_url) return;
            groupLoading.value = true;
            try {
                const payload = editingId.value
                    ? { config_id: editingId.value }
                    : { dashboard_url: form.dashboard_url, username: form.username, password: form.password };
                const res = await api.post('/api/rocketmq/consumer-groups', payload);
                groupOptions.value = (res || []).map(g => ({ label: g, value: g }));
            } catch (e) { message.error('获取消费组失败: ' + (e.message || '')); }
            groupLoading.value = false;
        }
        async function fetchTopics() {
            if (!form.dashboard_url) return;
            topicLoading.value = true;
            try {
                const payload = editingId.value
                    ? { config_id: editingId.value }
                    : { dashboard_url: form.dashboard_url, username: form.username, password: form.password };
                const res = await api.post('/api/rocketmq/topics', payload);
                topicOptions.value = (res || []).map(t => ({ label: t, value: t }));
            } catch (e) { message.error('获取Topic失败: ' + (e.message || '')); }
            topicLoading.value = false;
        }

        async function load() {
            loading.value = true;
            try { configs.value = await api.get('/api/rocketmq'); } catch {}
            loading.value = false;
        }
        onMounted(load);

        function openAdd() {
            editingId.value = null;
            Object.assign(form, { name: '', dashboard_url: '', username: '', password: '', consumer_group: '', topic: '', threshold: 1000, interval_sec: 30, notify_new_msg: false });
            groupOptions.value = []; topicOptions.value = [];
            showModal.value = true;
        }
        function openEdit(row) {
            editingId.value = row.id;
            Object.assign(form, { name: row.name, dashboard_url: row.dashboard_url, username: row.username, password: '', consumer_group: row.consumer_group, topic: row.topic, threshold: row.threshold, interval_sec: row.interval_sec, notify_new_msg: !!row.notify_new_msg });
            groupOptions.value = []; topicOptions.value = [];
            showModal.value = true;
        }
        function openClone(row) {
            editingId.value = null;
            Object.assign(form, { name: row.name + ' (副本)', dashboard_url: row.dashboard_url, username: row.username, password: '', consumer_group: row.consumer_group, topic: row.topic, threshold: row.threshold, interval_sec: row.interval_sec, notify_new_msg: !!row.notify_new_msg });
            groupOptions.value = []; topicOptions.value = [];
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
            h(NDataTable, { columns: columns.value, data: configs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 500 : undefined }),
            h(NModal, { show: showModal.value, onUpdateShow: v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑配置' : '新增配置', style: _isMobile.value ? 'width:95vw' : 'width:680px' }, () =>
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '名称', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.name, onUpdateValue: v => form.name = v, placeholder: '如: 订单系统MQ' }))),
                    h(NGi, null, () => h(NFormItem, { label: 'Dashboard URL', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.dashboard_url, onUpdateValue: v => form.dashboard_url = v, placeholder: 'http://host:port' }))),
                    h(NGi, null, () => h(NFormItem, { label: '用户名', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.username, onUpdateValue: v => form.username = v, placeholder: '可选' }))),
                    h(NGi, null, () => h(NFormItem, { label: '密码', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.password, onUpdateValue: v => form.password = v, type: 'password', showPasswordOn: 'click', placeholder: editingId.value ? '留空不修改' : '可选' }))),
                    h(NGi, null, () => h(NFormItem, { label: '消费组', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NSpace, { align: 'center', size: 'small', wrap: false }, () => [
                        h(NSelect, { value: form.consumer_group, onUpdateValue: v => form.consumer_group = v, options: groupOptions.value, filterable: true, tag: true, placeholder: '选择或输入消费组', style: 'flex:1;min-width:0', loading: groupLoading.value }),
                        h(NButton, { size: 'small', secondary: true, loading: groupLoading.value, onClick: fetchGroups }, () => '获取'),
                    ]))),
                    h(NGi, null, () => h(NFormItem, { label: 'Topic', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NSpace, { align: 'center', size: 'small', wrap: false }, () => [
                        h(NSelect, { value: form.topic, onUpdateValue: v => form.topic = v, options: topicOptions.value, filterable: true, tag: true, placeholder: '选择或输入Topic', style: 'flex:1;min-width:0', loading: topicLoading.value }),
                        h(NButton, { size: 'small', secondary: true, loading: topicLoading.value, onClick: fetchTopics }, () => '获取'),
                    ]))),
                    h(NGi, null, () => h(NFormItem, { label: '堆积阈值', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInputNumber, { value: form.threshold, onUpdateValue: v => form.threshold = v, min: 1, disabled: form.notify_new_msg }))),
                    h(NGi, null, () => h(NFormItem, { label: '检查间隔(秒)', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInputNumber, { value: form.interval_sec, onUpdateValue: v => form.interval_sec = v, min: 5 }))),
                    h(NGi, { span: gridCols.value }, () => h(NFormItem, { labelPlacement: 'left', label: '有新消息即告警' }, () => h(NSpace, { align: 'center' }, () => [
                        h(NSwitch, { value: form.notify_new_msg, onUpdateValue: v => form.notify_new_msg = v }),
                        h('span', { style: 'color:#999;font-size:12px' }, form.notify_new_msg ? '每次检查有新消息时即发送告警（根据消息ID去重）' : '关闭：按堆积量阈值告警'),
                    ]))),
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
        const detailRow = ref(null);
        const showDetail = ref(false);
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

        function onRowClick(row) {
            detailRow.value = row;
            showDetail.value = true;
        }

        const rowProps = (row) => ({ style: 'cursor:pointer', onClick: () => onRowClick(row) });

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
            h(NDataTable, { columns: columns.value, data: alerts.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 400 : undefined, rowProps }),
            total.value > pageSize ? h('div', { style: 'margin-top:16px;display:flex;justify-content:flex-end' },
                h(NPagination, { page: page.value, pageSize, itemCount: total.value, onUpdatePage: p => page.value = p })
            ) : null,
            // Detail modal
            h(NModal, {
                show: showDetail.value, 'onUpdate:show': v => showDetail.value = v,
                preset: 'card', title: '告警详情',
                style: _isMobile.value ? 'width:95vw' : 'width:520px',
            }, () => detailRow.value ? h(NDescriptions, { bordered: true, column: 1, labelPlacement: 'top', size: 'small' }, () => [
                h(NDescriptionsItem, { label: '时间' }, () => formatTime(detailRow.value.detected_at)),
                h(NDescriptionsItem, { label: '配置' }, () => detailRow.value.config_name),
                h(NDescriptionsItem, { label: '消费组' }, () => detailRow.value.consumer_group),
                h(NDescriptionsItem, { label: 'Topic' }, () => detailRow.value.topic),
                h(NDescriptionsItem, { label: '堆积量' }, () => h(NText, { type: 'error', strong: true }, () => String(detailRow.value.diff_total))),
                detailRow.value.message_body ? h(NDescriptionsItem, { label: '消息详情' }, () => h('pre', { style: 'white-space:pre-wrap;word-break:break-all;font-family:var(--font-mono);font-size:12px;margin:0;max-height:400px;overflow:auto' }, detailRow.value.message_body)) : null,
            ]) : null),
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
            h(NDataTable, { columns: columns.value, data: logs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 500 : undefined }),
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
            alert_field: '', alert_strategy: 'threshold', alert_condition: 'gt', alert_value: '',
            alert_delta_value: '', alert_delta_percent: '', alert_consecutive: 1,
            alert_rules: [],
            trigger_actions: [],
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
            Object.assign(form, { name: '', url: '', method: 'GET', headers_json: '{}', body: '', expected_status: 200, expected_field: '', expected_value: '', alert_field: '', alert_strategy: 'threshold', alert_condition: 'gt', alert_value: '', alert_delta_value: '', alert_delta_percent: '', alert_consecutive: 1, alert_rules: [], trigger_actions: [], timeout_sec: 10, interval_sec: 30 });
            showModal.value = true;
        }
        function openEdit(row) {
            editingId.value = row.id;
            const rules = parseAlertRules(row.alert_rules, row);
            Object.assign(form, { name: row.name, url: row.url, method: row.method, headers_json: row.headers_json || '{}', body: row.body || '', expected_status: row.expected_status, expected_field: row.expected_field || '', expected_value: row.expected_value || '', alert_field: row.alert_field || '', alert_strategy: row.alert_strategy || 'threshold', alert_condition: row.alert_condition || 'gt', alert_value: row.alert_value || '', alert_delta_value: row.alert_delta_value || '', alert_delta_percent: row.alert_delta_percent || '', alert_consecutive: row.alert_consecutive || 1, alert_rules: rules, trigger_actions: parseTriggerActions(row.trigger_actions), timeout_sec: row.timeout_sec, interval_sec: row.interval_sec });
            showModal.value = true;
        }
        function openClone(row) {
            editingId.value = null;
            const rules = parseAlertRules(row.alert_rules, row);
            Object.assign(form, { name: row.name + ' (副本)', url: row.url, method: row.method, headers_json: row.headers_json || '{}', body: row.body || '', expected_status: row.expected_status, expected_field: row.expected_field || '', expected_value: row.expected_value || '', alert_field: row.alert_field || '', alert_strategy: row.alert_strategy || 'threshold', alert_condition: row.alert_condition || 'gt', alert_value: row.alert_value || '', alert_delta_value: row.alert_delta_value || '', alert_delta_percent: row.alert_delta_percent || '', alert_consecutive: row.alert_consecutive || 1, alert_rules: rules, trigger_actions: parseTriggerActions(row.trigger_actions), timeout_sec: row.timeout_sec, interval_sec: row.interval_sec });
            showModal.value = true;
        }
        function normalizeAlertRule(rule) {
            const normalized = {
                name: rule.name || '',
                field: rule.field || rule.alert_field || '',
                strategy: rule.strategy || rule.alert_strategy || 'threshold',
                condition: rule.condition || rule.alert_condition || 'gt',
                value: rule.value || rule.alert_value || '',
                delta_value: rule.delta_value || rule.alert_delta_value || '',
                delta_percent: rule.delta_percent || rule.alert_delta_percent || '',
                consecutive: rule.consecutive || rule.alert_consecutive || 1,
            };
            normalized.value = normalizeRuleMetricInput(normalized.field, normalized.value);
            normalized.delta_value = normalizeRuleMetricInput(normalized.field, normalized.delta_value);
            return normalized;
        }
        function parseAlertRules(raw, row) {
            let parsed = [];
            if (Array.isArray(raw)) parsed = raw;
            else {
                try {
                    const value = JSON.parse(raw || '[]');
                    parsed = Array.isArray(value) ? value : [];
                } catch { parsed = []; }
            }
            parsed = parsed.map(normalizeAlertRule).filter(r => r.field);
            if (parsed.length === 0 && row && row.alert_field) {
                parsed.push(normalizeAlertRule({
                    name: row.alert_field,
                    field: row.alert_field,
                    strategy: row.alert_strategy,
                    condition: row.alert_condition,
                    value: row.alert_value,
                    delta_value: row.alert_delta_value,
                    delta_percent: row.alert_delta_percent,
                    consecutive: row.alert_consecutive,
                }));
            }
            return parsed;
        }
        function parseTriggerActions(raw) {
            if (Array.isArray(raw)) return raw;
            try {
                const parsed = JSON.parse(raw || '[]');
                return Array.isArray(parsed) ? parsed.map(normalizeTriggerAction) : [];
            } catch { return []; }
        }
        function normalizeTriggerAction(action) {
            return {
                name: action.name || '',
                type: action.type || 'command',
                command: action.command || '',
                url: action.url || '',
                method: action.method || 'GET',
                headers_json: action.headers_json || '{}',
                body: action.body || '',
                timeout_sec: action.timeout_sec || 30,
                notify_max_chars: action.notify_max_chars || 2000,
                enabled: action.enabled !== false,
            };
        }
        function payload() {
            const actions = (form.trigger_actions || []).map(normalizeTriggerAction).filter(a =>
                a.name || a.command || a.url
            );
            const rules = (form.alert_rules || []).map(normalizeAlertRule).filter(r => r.field);
            const firstRule = rules[0] || normalizeAlertRule({
                field: form.alert_field,
                strategy: form.alert_strategy,
                condition: form.alert_condition,
                value: form.alert_value,
                delta_value: form.alert_delta_value,
                delta_percent: form.alert_delta_percent,
                consecutive: form.alert_consecutive,
            });
            return {
                ...form,
                alert_rules: JSON.stringify(rules),
                alert_field: firstRule.field || '',
                alert_strategy: firstRule.strategy || 'threshold',
                alert_condition: firstRule.condition || 'gt',
                alert_value: firstRule.value || '',
                alert_delta_value: firstRule.delta_value || '',
                alert_delta_percent: firstRule.delta_percent || '',
                alert_consecutive: firstRule.consecutive || 1,
                trigger_actions: JSON.stringify(actions),
            };
        }
        async function save() {
            try {
                if (editingId.value) {
                    await api.put('/api/health-checks/' + editingId.value, payload());
                } else {
                    await api.post('/api/health-checks', payload());
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
        function addTriggerAction(type = 'command') {
            form.trigger_actions.push(normalizeTriggerAction({
                name: type === 'http' ? '抓取诊断接口' : '执行诊断命令',
                type,
                timeout_sec: 30,
            }));
        }
        function addPprofTemplate() {
            const templates = [
                ['抓取 heap', 'curl -s "http://localhost:8080/debug/pprof/heap" > /tmp/heap.pb.gz'],
                ['pprof top', 'go tool pprof -top -sample_index=inuse_space /tmp/heap.pb.gz'],
                ['pprof cum', 'go tool pprof -top -cum -sample_index=inuse_space /tmp/heap.pb.gz'],
                ['pprof traces', 'go tool pprof -traces -sample_index=inuse_space -nodefraction=0 /tmp/heap.pb.gz | head -120'],
            ];
            templates.forEach(([name, command]) => form.trigger_actions.push(normalizeTriggerAction({
                name, type: 'command', command, timeout_sec: 60, notify_max_chars: 2000,
            })));
        }
        function removeTriggerAction(index) {
            form.trigger_actions.splice(index, 1);
        }
        function addAlertRule(rule = {}) {
            form.alert_rules.push(normalizeAlertRule(rule));
        }
        function removeAlertRule(index) {
            form.alert_rules.splice(index, 1);
        }
        function addTTPOSMetricRules() {
            form.alert_rules.push(
                normalizeAlertRule({ name: 'heap 当前占用过高', field: 'data.heap_alloc_kb', strategy: 'sustained', condition: 'gt', value: '1000', consecutive: 3 }),
                normalizeAlertRule({ name: 'heap 对象数过高', field: 'data.heap_objects', strategy: 'sustained', condition: 'gt', value: '9000000', consecutive: 3 }),
                normalizeAlertRule({ name: 'goroutine 数过高', field: 'data.goroutines', strategy: 'sustained', condition: 'gt', value: '1500', consecutive: 3 }),
                normalizeAlertRule({ name: 'heap 持续上升', field: 'data.heap_alloc_kb', strategy: 'continuous_increase', condition: 'gt', value: '800', consecutive: 5 }),
                normalizeAlertRule({ name: '字体处理排队', field: 'data.imgFontSemaphore.semaphore_length', strategy: 'sustained', condition: 'gt', value: '0', consecutive: 2 }),
            );
        }

        const columns = useColumns([
            { title: '名称', key: 'name', width: 120 },
            { title: 'URL', key: 'url', ellipsis: { tooltip: true }, _hideOnMobile: true },
            { title: '方法', key: 'method', width: 70 },
            { title: '异常规则', key: 'alert_rule', width: 260, _hideOnMobile: true, render: row => {
                const rules = parseAlertRules(row.alert_rules, row);
                if (!rules.length) return h(NText, { depth: 3, style: 'font-size:12px' }, () => '-');
                if (rules.length > 1) return h(NText, { depth: 3, style: 'font-size:12px' }, () => rules.length + ' 条: ' + rules.map(r => r.name || r.field).join(' / '));
                return h(NText, { depth: 3, style: 'font-size:12px' }, () => formatAlertRule({ ...row, alert_field: rules[0].field, alert_strategy: rules[0].strategy, alert_condition: rules[0].condition, alert_value: rules[0].value, alert_delta_value: rules[0].delta_value, alert_delta_percent: rules[0].delta_percent, alert_consecutive: rules[0].consecutive }));
            } },
            { title: '触发', key: 'trigger_actions', width: 80, _hideOnMobile: true, render: row => {
                const actions = parseTriggerActions(row.trigger_actions);
                return actions.length ? h(NTag, { size: 'small', type: 'warning', bordered: false }, () => actions.length + ' 个') : h(NText, { depth: 3 }, () => '-');
            }},
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
        const alertConditionOptions = [
            { label: '> 大于', value: 'gt' },
            { label: '>= 大于等于', value: 'gte' },
            { label: '< 小于', value: 'lt' },
            { label: '<= 小于等于', value: 'lte' },
            { label: '== 等于', value: 'eq' },
            { label: '!= 不等于', value: 'ne' },
            { label: '包含', value: 'contains' },
            { label: '不包含', value: 'not_contains' },
            { label: '为空', value: 'empty' },
            { label: '不为空', value: 'not_empty' },
        ];
        const alertStrategyOptions = [
            { label: '单次阈值', value: 'threshold' },
            { label: '连续命中阈值', value: 'sustained' },
            { label: '突增', value: 'increase' },
            { label: '连续上升', value: 'continuous_increase' },
        ];
        const ruleNeedsValue = rule => !['empty', 'not_empty'].includes(rule.condition);
        const ruleUsesThreshold = rule => ['threshold', 'sustained', 'increase', 'continuous_increase'].includes(rule.strategy);
        const ruleUsesDelta = rule => rule.strategy === 'increase';
        const ruleUsesConsecutive = rule => ['sustained', 'continuous_increase'].includes(rule.strategy);
        const isKBMetricField = field => /(^|\.)[^.]+_kb$/.test(String(field || ''));
        function normalizeRuleMetricInput(field, value) {
            if (!isKBMetricField(field) || value === '' || value == null) return value || '';
            const raw = String(value).replace(/,/g, '');
            if (raw === '1024000') return '1000';
            if (raw === '150000000') return '1000';
            if (raw === '819200') return '800';
            if (raw === '1500000') return '1464.84';
            if (raw === '1200000') return '1171.88';
            return String(value);
        }
        function formatRuleMetricValue(field, value) {
            if (value === '' || value == null) return '';
            const numeric = Number(String(value).replace(/,/g, ''));
            if (!Number.isFinite(numeric)) return value;
            if (isKBMetricField(field)) return formatNumber(numeric) + ' MB';
            return formatNumber(numeric);
        }
        function ruleInputValue(rule, key = 'value') {
            return normalizeRuleMetricInput(rule.field, rule[key]);
        }
        function setRuleInputValue(rule, value, key = 'value') {
            rule[key] = value == null ? '' : String(value);
        }
        function ruleValuePlaceholder(rule) {
            return isKBMetricField(rule.field) ? '当前值阈值，单位 MB，如 1000' : (rule.strategy === 'threshold' || rule.strategy === 'sustained' ? '当前值阈值，如 2000' : '当前值阈值，可选');
        }
        function ruleValueInput(rule, key = 'value', placeholder = '') {
            const input = h(NInput, { value: ruleInputValue(rule, key), onUpdateValue: v => setRuleInputValue(rule, v, key), placeholder });
            if (!isKBMetricField(rule.field)) return input;
            return h(NInputGroup, null, () => [
                input,
                h(NButton, { disabled: true, secondary: true, style: 'width:64px;pointer-events:none' }, () => 'MB'),
            ]);
        }
        function formatAlertRule(row) {
            const strategyLabels = { threshold: '单次', sustained: '连续命中', increase: '突增', continuous_increase: '连续上升' };
            const strategy = row.alert_strategy || 'threshold';
            let text = (strategyLabels[strategy] || strategy) + ': ' + row.alert_field;
            if (strategy === 'increase') {
                const parts = [];
                if (row.alert_delta_value) parts.push('涨幅>=' + formatRuleMetricValue(row.alert_field, row.alert_delta_value));
                if (row.alert_delta_percent) parts.push('涨幅率>=' + row.alert_delta_percent + '%');
                if (row.alert_value) parts.push((row.alert_condition || 'gt') + ' ' + formatRuleMetricValue(row.alert_field, row.alert_value));
                return text + ' ' + (parts.join(' 且/或 ') || '比上次上升');
            }
            if (strategy === 'continuous_increase') {
                text += ' 连续上升 ' + (row.alert_consecutive || 1) + ' 次';
                if (row.alert_value) text += ' 且 ' + (row.alert_condition || 'gt') + ' ' + formatRuleMetricValue(row.alert_field, row.alert_value);
                return text;
            }
            if (strategy === 'sustained') {
                return text + ' ' + (row.alert_condition || 'gt') + ' ' + formatRuleMetricValue(row.alert_field, row.alert_value || '') + ' 连续 ' + (row.alert_consecutive || 1) + ' 次';
            }
            return text + ' ' + (row.alert_condition || 'gt') + ' ' + formatRuleMetricValue(row.alert_field, row.alert_value || '');
        }
        const triggerActionTypeOptions = [
            { label: '命令', value: 'command' },
            { label: 'HTTP', value: 'http' },
        ];
        function baseHealthFields() {
            return [
                h(NFormItem, { label: '名称' }, () => h(NInput, { value: form.name, onUpdateValue: v => form.name = v, placeholder: '服务名称' })),
                h(NFormItem, { label: 'URL' }, () => h(NInput, { value: form.url, onUpdateValue: v => form.url = v, placeholder: 'https://example.com/health' })),
                h(NGrid, { cols: _isMobile.value ? 1 : 2, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '方法' }, () => h(NSelect, { value: form.method, onUpdateValue: v => form.method = v, options: methodOptions }))),
                    h(NGi, null, () => h(NFormItem, { label: '期望状态码' }, () => h(NInputNumber, { value: form.expected_status, onUpdateValue: v => form.expected_status = v, min: 100, max: 599, style: 'width:100%' }))),
                ]),
                h(NFormItem, { label: '请求头' }, () => h(NInput, { type: 'textarea', value: form.headers_json, onUpdateValue: v => form.headers_json = v, placeholder: '{"Authorization": "Bearer token"}', rows: 3 })),
                form.method !== 'GET' && form.method !== 'HEAD' ? h(NFormItem, { label: '请求体' }, () => h(NInput, { type: 'textarea', value: form.body, onUpdateValue: v => form.body = v, placeholder: '请求体内容', rows: 3 })) : null,
                h(NDivider, { style: 'margin:16px 0' }),
                h(NFormItem, { label: '期望字段' }, () => h(NInput, { value: form.expected_field, onUpdateValue: v => form.expected_field = v, placeholder: '如: code 或 data.status，留空则只检查状态码' })),
                h(NFormItem, { label: '期望值' }, () => h(NInput, { value: form.expected_value, onUpdateValue: v => form.expected_value = v, placeholder: '如: 0, UP, ok' })),
                h(NDivider, { style: 'margin:16px 0' }),
                h(NGrid, { cols: _isMobile.value ? 1 : 2, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '超时(秒)' }, () => h(NInputNumber, { value: form.timeout_sec, onUpdateValue: v => form.timeout_sec = v, min: 1, max: 300, style: 'width:100%' }))),
                    h(NGi, null, () => h(NFormItem, { label: '间隔(秒)' }, () => h(NInputNumber, { value: form.interval_sec, onUpdateValue: v => form.interval_sec = v, min: 5, max: 3600, style: 'width:100%' }))),
                ]),
            ];
        }
        function triggerActionFields() {
            return [
                h(NFormItem, { label: '触发操作' }, () => h('div', { style: 'width:100%;display:flex;flex-direction:column;gap:10px' }, [
                    h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' }, [
                        h(NButton, { size: 'small', secondary: true, onClick: () => addTriggerAction('command') }, () => '+ 命令'),
                        h(NButton, { size: 'small', secondary: true, onClick: () => addTriggerAction('http') }, () => '+ HTTP'),
                        h(NButton, { size: 'small', secondary: true, type: 'warning', onClick: addPprofTemplate }, () => '+ pprof模板'),
                    ]),
                    (form.trigger_actions || []).length === 0 ? h(NText, { depth: 3, style: 'font-size:12px' }, () => '异常首次命中时执行；恢复后再次异常会重新执行。') : null,
                    ...(form.trigger_actions || []).map((action, index) => h('div', { style: 'border:1px solid rgba(128,128,128,.22);border-radius:6px;padding:10px;display:flex;flex-direction:column;gap:8px' }, [
                        h('div', { style: 'display:flex;justify-content:space-between;align-items:flex-end;gap:10px;flex-wrap:wrap' }, [
                            h('div', { style: _isMobile.value ? 'display:flex;flex-direction:column;gap:8px;flex:1 1 100%;min-width:0' : 'display:grid;grid-template-columns:minmax(150px,1fr) 132px 118px 150px;gap:8px;align-items:end;flex:1 1 auto;min-width:0' }, [
                                h(NInput, { value: action.name, onUpdateValue: v => action.name = v, placeholder: '动作名称' }),
                                h(NSelect, { value: action.type, onUpdateValue: v => action.type = v, options: triggerActionTypeOptions }),
                                h('div', { style: 'display:flex;flex-direction:column;gap:4px;min-width:0' }, [
                                    h(NText, { depth: 3, style: 'font-size:12px;line-height:1' }, () => '超时(秒)'),
                                    h(NInputNumber, { value: action.timeout_sec, onUpdateValue: v => action.timeout_sec = v, min: 1, max: 300, style: 'width:100%' }),
                                ]),
                                h('div', { style: 'display:flex;flex-direction:column;gap:4px;min-width:0' }, [
                                    h(NText, { depth: 3, style: 'font-size:12px;line-height:1' }, () => '飞书截断字数'),
                                    h(NInputNumber, { value: action.notify_max_chars, onUpdateValue: v => action.notify_max_chars = v, min: 100, max: 50000, step: 100, style: 'width:100%' }),
                                ]),
                            ]),
                            h('div', { style: 'display:flex;align-items:center;justify-content:flex-end;gap:8px;flex:0 0 auto;padding-bottom:2px' }, [
                                h(NSwitch, { value: action.enabled !== false, onUpdateValue: v => action.enabled = v, size: 'small' }),
                                h(NButton, { size: 'tiny', secondary: true, type: 'error', onClick: () => removeTriggerAction(index) }, () => '删除'),
                            ]),
                        ]),
                        action.type === 'http' ? h('div', { style: 'display:flex;flex-direction:column;gap:8px' }, [
                            h(NInputGroup, null, () => [
                                h(NSelect, { value: action.method || 'GET', onUpdateValue: v => action.method = v, options: methodOptions, style: 'width:110px' }),
                                h(NInput, { value: action.url, onUpdateValue: v => action.url = v, placeholder: 'http://host/path' }),
                            ]),
                            h(NInput, { type: 'textarea', value: action.headers_json || '{}', onUpdateValue: v => action.headers_json = v, placeholder: '请求头 JSON', rows: 2 }),
                            action.method !== 'GET' && action.method !== 'HEAD' ? h(NInput, { type: 'textarea', value: action.body || '', onUpdateValue: v => action.body = v, placeholder: '请求体', rows: 2 }) : null,
                        ]) : h(NInput, { type: 'textarea', value: action.command, onUpdateValue: v => action.command = v, placeholder: '如: go tool pprof -top -sample_index=inuse_space /tmp/heap.pb.gz', rows: 3 }),
                    ])),
                ])),
            ];
        }
        function alertHealthFields() {
            return [
                h(NFormItem, { label: '异常规则' }, () => h('div', { style: 'width:100%;display:flex;flex-direction:column;gap:10px' }, [
                    h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' }, [
                        h(NButton, { size: 'small', secondary: true, onClick: () => addAlertRule({ name: '新规则', strategy: 'sustained', condition: 'gt', consecutive: 3 }) }, () => '+ 规则'),
                        h(NButton, { size: 'small', secondary: true, type: 'info', onClick: addTTPOSMetricRules }, () => '+ ttpos模板'),
                    ]),
                    (form.alert_rules || []).length === 0 ? h(NText, { depth: 3, style: 'font-size:12px' }, () => '可添加多条规则；任意一条命中都会告警。') : null,
                    ...(form.alert_rules || []).map((rule, index) => h('div', { style: 'border:1px solid rgba(128,128,128,.22);border-radius:6px;padding:10px;display:flex;flex-direction:column;gap:8px' }, [
                        h(NGrid, { cols: _isMobile.value ? 1 : 2, xGap: 8, yGap: 8 }, () => [
                            h(NGi, null, () => h(NInput, { value: rule.name, onUpdateValue: v => rule.name = v, placeholder: '规则名称，如 heap 当前占用过高' })),
                            h(NGi, null, () => h(NInput, { value: rule.field, onUpdateValue: v => rule.field = v, placeholder: '字段，如 data.heap_alloc_kb' })),
                            h(NGi, null, () => h(NSelect, { value: rule.strategy, onUpdateValue: v => rule.strategy = v, options: alertStrategyOptions })),
                            ruleUsesThreshold(rule) ? h(NGi, null, () => h(NSelect, { value: rule.condition, onUpdateValue: v => rule.condition = v, options: alertConditionOptions })) : null,
                            ruleUsesThreshold(rule) && ruleNeedsValue(rule) ? h(NGi, null, () => ruleValueInput(rule, 'value', ruleValuePlaceholder(rule))) : null,
                            ruleUsesDelta(rule) ? h(NGi, null, () => ruleValueInput(rule, 'delta_value', isKBMetricField(rule.field) ? '变化量>=，单位 MB，如 200' : '变化量>=，如 500')) : null,
                            ruleUsesDelta(rule) ? h(NGi, null, () => h(NInput, { value: rule.delta_percent, onUpdateValue: v => rule.delta_percent = v, placeholder: '变化率>=，如 30' })) : null,
                            ruleUsesConsecutive(rule) ? h(NGi, null, () => h(NInputNumber, { value: rule.consecutive, onUpdateValue: v => rule.consecutive = v, min: 1, max: 100, style: 'width:100%', placeholder: '连续次数' })) : null,
                        ]),
                        h('div', { style: 'display:flex;justify-content:space-between;align-items:center;gap:8px' }, [
                            h(NText, { depth: 3, style: 'font-size:12px' }, () => {
                                if (rule.strategy === 'increase') return '突增：与上一次采样比较；变化量和变化率任一满足即可。';
                                if (rule.strategy === 'continuous_increase') return '连续上升：连续多次比上一次采样更高，可叠加当前值阈值。';
                                if (rule.strategy === 'sustained') return '连续命中阈值：连续多次满足条件才告警。';
                                return '单次阈值：本次满足条件即告警。';
                            }),
                            h(NButton, { size: 'tiny', secondary: true, type: 'error', onClick: () => removeAlertRule(index) }, () => '删除'),
                        ]),
                    ])),
                ])),
                h(NDivider, { style: 'margin:8px 0 16px' }),
                ...triggerActionFields(),
            ];
        }

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, '健康检查'),
                h(NButton, { type: 'primary', size: 'small', onClick: openAdd }, () => '+ 添加'),
            ]),
            h(NDataTable, { columns: columns.value, data: checks.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 500 : undefined }),
            h(NModal, { show: showModal.value, 'onUpdate:show': v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑健康检查' : '添加健康检查', style: _isMobile.value ? 'width:95vw' : 'width:1520px;max-width:98vw', segmented: true }, () =>
                h(NForm, { labelPlacement: _isMobile.value ? 'top' : 'left', labelWidth: _isMobile.value ? undefined : 100 }, () => [
                    h(NGrid, { cols: _isMobile.value ? 1 : 2, xGap: 24, yGap: 0 }, () => [
                        h(NGi, null, () => h('div', { style: 'min-width:0' }, baseHealthFields())),
                        h(NGi, null, () => h('div', { style: 'min-width:0' }, alertHealthFields())),
                    ]),
                    h('div', { style: 'display:flex;justify-content:flex-end;gap:8px;margin-top:12px;padding-top:12px;border-top:1px solid rgba(128,128,128,.2)' }, [
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
        const showDetail = ref(false);
        const detailRow = ref(null);
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

        function openDetail(row) {
            detailRow.value = row;
            showDetail.value = true;
        }
        const rowProps = row => ({
            style: 'cursor:pointer',
            onClick: () => openDetail(row),
        });
        const responseFieldDescriptions = {
            code: '业务状态码，通常 0 表示接口调用成功。',
            message: '接口返回消息，用来快速判断接口层是否成功。',
            'data.alloc_kb': '当前已分配且仍在使用的堆内存，按 MB 展示，通常等同 heap_alloc_kb。',
            'data.heap_alloc_kb': 'Go 堆上当前仍被对象占用的内存，按 MB 展示，是观察进程内存压力的核心指标。',
            'data.heap_inuse_kb': '已经从系统拿到并正在被堆使用的内存页，按 MB 展示。',
            'data.heap_idle_kb': '堆中空闲但尚未全部归还系统的内存，按 MB 展示。',
            'data.heap_released_kb': '已经归还给操作系统的堆内存，按 MB 展示。',
            'data.heap_sys_kb': 'Go 堆从操作系统申请到的总内存，按 MB 展示。',
            'data.heap_objects': '当前堆上存活对象数量。持续上涨通常说明对象未释放或缓存增长。',
            'data.goroutines': '当前 goroutine 数量。异常上涨可能说明协程泄漏、请求阻塞或后台任务堆积。',
            'data.gc_cpu_fraction': 'GC 消耗 CPU 的比例。持续偏高说明 GC 压力较大。',
            'data.gc_cycles': 'GC 周期数。',
            'data.num_gc': 'GC 执行次数。',
            'data.next_gc_kb': '下次触发 GC 的堆目标值，按 MB 展示。',
            'data.total_alloc_kb': '进程启动以来累计分配的内存，按 MB 展示，只增不减。',
            'data.sys_kb': 'Go runtime 从系统申请的总内存，按 MB 展示。',
            'data.stack_inuse_kb': 'goroutine 栈当前使用内存，按 MB 展示。',
            'data.stack_sys_kb': 'goroutine 栈从系统申请的内存，按 MB 展示。',
            'data.mspan_inuse_kb': 'runtime mspan 元数据当前使用内存，按 MB 展示。',
            'data.mspan_sys_kb': 'runtime mspan 元数据系统内存，按 MB 展示。',
            'data.mcache_inuse_kb': 'runtime mcache 当前使用内存，按 MB 展示。',
            'data.mcache_sys_kb': 'runtime mcache 系统内存，按 MB 展示。',
            'data.gcsys_kb': 'GC 元数据占用的系统内存，按 MB 展示。',
            'data.other_sys_kb': '其他 runtime 系统内存，按 MB 展示。',
            'data.pause_total_ns': 'GC 累计暂停时间，单位纳秒。',
            'data.num_forced_gc': '手动触发 GC 的次数。',
            'data.enable_gc': 'GC 是否启用。',
            'data.debug_gc': '是否开启 GC 调试。',
            'data.imgFontSemaphore.active_count': '图片/字体处理当前活跃任务数。',
            'data.imgFontSemaphore.available': '图片/字体处理可用并发额度。',
            'data.imgFontSemaphore.max_concurrent': '图片/字体处理最大并发数。',
            'data.imgFontSemaphore.semaphore_length': '图片/字体处理等待队列长度，大于 0 表示有排队堵塞。',
            'data.imgFontSemaphore.memory_usage': '图片/字体相关缓存或处理占用内存，按 MB 展示。',
            'data.imgFontSemaphore.font_count': '已加载字体数量。',
            'data.imgFontSemaphore.access_cache_count': '访问缓存条目数量。',
            'data.imgFontSemaphore.width_cache_count': '宽度缓存条目数量。',
        };
        function parseLogJSON(text) {
            const raw = String(text || '').trim();
            if (!raw) return { text: '', json: null };
            if (!(raw.startsWith('{') || raw.startsWith('['))) return { text: raw, json: null };
            try {
                const json = JSON.parse(raw);
                return { text: JSON.stringify(json, null, 2), json };
            } catch {
                return { text: raw, json: null };
            }
        }
        function flattenJSONFields(value, prefix = '', out = []) {
            if (value == null || typeof value !== 'object') {
                if (prefix) out.push({ path: prefix, value });
                return out;
            }
            if (Array.isArray(value)) {
                out.push({ path: prefix || '[]', value: '[' + value.length + ' items]' });
                return out;
            }
            Object.keys(value).forEach(key => {
                const path = prefix ? prefix + '.' + key : key;
                flattenJSONFields(value[key], path, out);
            });
            return out;
        }
        function formatNumberWithUnit(value, divisor, unit) {
            const n = Number(value);
            if (!Number.isFinite(n)) return String(value);
            return (n / divisor).toLocaleString(undefined, { maximumFractionDigits: 2 }) + ' ' + unit;
        }
        function formatFieldValue(path, value) {
            if (value == null) return '-';
            if (typeof value === 'number' && /_kb$/.test(path)) return formatNumberWithUnit(value, 1024, 'MB');
            if (typeof value === 'number' && path === 'data.imgFontSemaphore.memory_usage') return formatNumberWithUnit(value, 1024 * 1024, 'MB');
            if (typeof value === 'number') return Number.isInteger(value) ? value.toLocaleString() : String(value);
            if (typeof value === 'boolean') return value ? 'true' : 'false';
            return String(value);
        }
        const defaultResponseDescriptionPaths = [
            'data.heap_alloc_kb',
            'data.heap_objects',
            'data.goroutines',
            'data.heap_inuse_kb',
            'data.heap_idle_kb',
            'data.heap_released_kb',
            'data.gc_cpu_fraction',
            'data.total_alloc_kb',
            'data.imgFontSemaphore.semaphore_length',
            'data.imgFontSemaphore.active_count',
            'data.imgFontSemaphore.available',
            'data.imgFontSemaphore.memory_usage',
        ];
        function fieldDescriptionBlock(json, responseText) {
            if (!json && !String(responseText || '').trim()) return null;
            let rows = json ? flattenJSONFields(json)
                .filter(item => responseFieldDescriptions[item.path])
                .slice(0, 80) : [];
            if (!rows.length) {
                rows = defaultResponseDescriptionPaths.map(path => ({ path, value: '-' }));
            }
            return h('div', { style: 'display:flex;flex-direction:column;gap:6px;min-width:0' }, [
                h('div', { style: 'font-weight:600' }, '字段说明'),
                h('div', { style: 'max-height:260px;overflow:auto;border:1px solid rgba(128,128,128,.18);border-radius:6px;background:rgba(128,128,128,.05)' }, [
                    h('table', { style: 'width:100%;border-collapse:collapse;font-size:12px;line-height:1.45' }, [
                        h('tbody', null, rows.map(item => h('tr', { style: 'border-bottom:1px solid rgba(128,128,128,.12)' }, [
                            h('td', { style: 'width:34%;vertical-align:top;padding:7px 8px;font-family:var(--font-mono);color:#63e2b7;word-break:break-word' }, item.path),
                            h('td', { style: 'width:18%;vertical-align:top;padding:7px 8px;font-family:var(--font-mono);opacity:.85;word-break:break-word' }, formatFieldValue(item.path, item.value)),
                            h('td', { style: 'vertical-align:top;padding:7px 8px;opacity:.82;word-break:break-word' }, responseFieldDescriptions[item.path]),
                        ]))),
                    ]),
                ]),
            ]);
        }
        function detailSections(row) {
            let error = String(row?.error || '');
            let diagnostic = String(row?.diagnostic_output || '');
            const response = parseLogJSON(row?.response);
            const markers = ['\n\n诊断输出:\n', '\n诊断输出:\n', '诊断输出:\n'];
            if (!diagnostic) {
                for (const marker of markers) {
                    const index = error.indexOf(marker);
                    if (index >= 0) {
                        diagnostic = error.slice(index + marker.length);
                        error = error.slice(0, index);
                        break;
                    }
                }
            }
            return {
                error: error.trim(),
                diagnostic: diagnostic.trim(),
                response: response.text,
                responseJSON: response.json,
            };
        }
        function logBlock(text, maxHeight = 360) {
            return h('pre', { style: `white-space:pre-wrap;word-break:break-word;font-family:var(--font-mono);font-size:12px;line-height:1.55;margin:0;max-height:${maxHeight}px;overflow:auto;background:rgba(128,128,128,.08);border:1px solid rgba(128,128,128,.18);border-radius:6px;padding:10px` }, text || '-');
        }
        function sectionBlock(title, text, maxHeight = 360, emptyText = '-') {
            return h('div', { style: 'display:flex;flex-direction:column;gap:6px;min-width:0' }, [
                h('div', { style: 'font-weight:600' }, title),
                logBlock(text || emptyText, maxHeight),
            ]);
        }
        function renderDetail() {
            if (!detailRow.value) return null;
            const sections = detailSections(detailRow.value);
            const shellStyle = _isMobile.value
                ? 'display:flex;flex-direction:column;gap:14px;max-height:calc(100vh - 180px);overflow:auto'
                : 'display:flex;flex-direction:column;gap:14px;max-height:calc(100vh - 190px);overflow:auto';
            const panelStyle = _isMobile.value
                ? 'display:flex;flex-direction:column;gap:14px;min-width:0'
                : 'display:flex;flex-direction:column;gap:14px;min-width:0';
            const topGridStyle = _isMobile.value
                ? 'display:flex;flex-direction:column;gap:14px;min-width:0'
                : 'display:grid;grid-template-columns:minmax(0,0.9fr) minmax(0,1.1fr);gap:16px;align-items:start;min-width:0';
            return h('div', { style: shellStyle }, [
                h('div', { style: topGridStyle }, [
                    h('div', { style: panelStyle }, [
                        h(NDescriptions, { bordered: true, column: 2, labelPlacement: 'top', size: 'small' }, () => [
                            h(NDescriptionsItem, { label: '时间' }, () => formatTime(detailRow.value.detected_at)),
                            h(NDescriptionsItem, { label: '服务' }, () => detailRow.value.check_name || '-'),
                            h(NDescriptionsItem, { label: '状态' }, () => h(NTag, { type: detailRow.value.status === 'up' ? 'success' : 'error', size: 'small', bordered: false }, () => (detailRow.value.status || '').toUpperCase())),
                            h(NDescriptionsItem, { label: 'HTTP' }, () => String(detailRow.value.http_status || 0)),
                            h(NDescriptionsItem, { label: '延迟' }, () => (detailRow.value.latency_ms || 0) + 'ms'),
                            h(NDescriptionsItem, { label: '日志ID' }, () => String(detailRow.value.id || '-')),
                        ]),
                        sectionBlock('错误', sections.error, 260),
                    ]),
                    h('div', { style: panelStyle }, [
                        sectionBlock('诊断输出', sections.diagnostic, 620, '本条日志没有保存诊断输出。只有配置了触发操作并命中首次异常时，才会写入这里。'),
                    ]),
                ]),
                sectionBlock('响应 / 规则跟踪', sections.response, 360),
                fieldDescriptionBlock(sections.responseJSON, sections.response),
            ]);
        }

        const columns = useColumns([
            { title: '时间', key: 'detected_at', width: 150, render: row => h('span', { style: 'font-size:12px;opacity:0.65' }, formatTime(row.detected_at)) },
            { title: '服务', key: 'check_name', width: 120 },
            { title: '状态', key: 'status', width: 80, render: row => h(NTag, { type: row.status === 'up' ? 'success' : 'error', size: 'small', bordered: false }, () => row.status.toUpperCase()) },
            { title: 'HTTP', key: 'http_status', width: 70, _hideOnMobile: true },
            { title: '延迟', key: 'latency_ms', width: 80, render: row => row.latency_ms + 'ms' },
            { title: '错误', key: 'error', ellipsis: { tooltip: true }, _hideOnMobile: true, render: row => row.error ? h('span', { style: 'cursor:pointer;text-decoration:underline dotted;text-underline-offset:3px', onClick: e => { e.stopPropagation(); openDetail(row); } }, truncate(row.error.replace(/\n/g, ' / '), 120)) : h(NText, { depth: 3 }, () => '-') },
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
            h(NDataTable, { columns: columns.value, data: logs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 400 : undefined, rowProps }),
            total.value > pageSize ? h('div', { style: 'margin-top:16px;display:flex;justify-content:flex-end' },
                h(NPagination, { page: page.value, pageSize, itemCount: total.value, onUpdatePage: p => page.value = p })
            ) : null,
            h(NModal, { show: showDetail.value, 'onUpdate:show': v => showDetail.value = v, preset: 'card', title: '检查日志详情', style: _isMobile.value ? 'width:95vw' : 'width:1280px;max-width:94vw', segmented: true }, () => renderDetail()),
        ]);
    }
});

// --- Grafana ---
const GrafanaPage = defineComponent({
    setup() {
        const configs = ref([]);
        const ruleDefs = ref([]);
        const loading = ref(true);
        const showModal = ref(false);
        const editingId = ref(null);
        const form = reactive({ name: '', grafana_url: '', username: '', password: '', datasource_uid: '', auto_rules: [], webhook_url: '', webhook_secret: '', interval_sec: 60 });
        const saving = ref(false);
        const provisioning = ref(null);
        const datasources = ref([]);
        const dsLoading = ref(false);
        const message = useMessage();

        async function fetchDatasources() {
            dsLoading.value = true;
            try {
                let res;
                if (editingId.value) {
                    res = await api.get('/api/grafana/' + editingId.value + '/datasources');
                } else {
                    if (!form.grafana_url || !form.username) { dsLoading.value = false; return; }
                    res = await api.post('/api/grafana/datasources', { grafana_url: form.grafana_url, username: form.username, password: form.password });
                }
                datasources.value = (res || []).map(d => ({ label: d.name + ' (' + d.uid + ')', value: d.uid }));
            } catch (e) { message.error('获取数据源失败: ' + (e.message || '')); }
            dsLoading.value = false;
        }

        async function load() {
            loading.value = true;
            try { configs.value = await api.get('/api/grafana'); } catch {}
            loading.value = false;
        }
        async function loadRuleDefs() {
            try { ruleDefs.value = await api.get('/api/grafana/rule-defs'); } catch {}
        }
        onMounted(() => { load(); loadRuleDefs(); });

        const ruleOptions = computed(() => ruleDefs.value.map(r => ({ label: r.title + ' (' + r.key + ')', value: r.key })));

        async function openAdd() {
            editingId.value = null;
            let secret = '';
            try { const res = await api.post('/api/grafana/generate-secret'); secret = res.secret || ''; } catch {}
            Object.assign(form, { name: '', grafana_url: '', username: '', password: '', datasource_uid: '', auto_rules: [], webhook_url: location.origin, webhook_secret: secret, interval_sec: 60 });
            datasources.value = [];
            showModal.value = true;
        }
        function openEdit(row) {
            editingId.value = row.id;
            let rules = [];
            try { rules = JSON.parse(row.auto_rules || '[]'); } catch {}
            Object.assign(form, { name: row.name, grafana_url: row.grafana_url, username: row.username, password: '', datasource_uid: row.datasource_uid, auto_rules: rules, webhook_url: row.webhook_url, webhook_secret: row.webhook_secret || '', interval_sec: row.interval_sec });
            datasources.value = [];
            showModal.value = true;
        }
        async function save() {
            if (!form.name || !form.grafana_url) { message.error('请填写名称和 Grafana URL'); return; }
            saving.value = true;
            try {
                if (editingId.value) await api.put('/api/grafana/' + editingId.value, form);
                else await api.post('/api/grafana', form);
                showModal.value = false;
                message.success(editingId.value ? '已更新' : '已创建');
                load();
            } catch (e) { message.error(e.message || '保存失败'); }
            saving.value = false;
        }
        async function del(row) {
            try { await api.del('/api/grafana/' + row.id); message.success('已删除'); load(); } catch (e) { message.error(e.message); }
        }
        async function toggle(row) {
            try { await api.post('/api/grafana/' + row.id + '/toggle'); load(); } catch (e) { message.error(e.message); }
        }
        async function test(row) {
            try {
                const res = await api.post('/api/grafana/' + row.id + '/test');
                res.ok ? message.success(res.message) : message.error(res.message);
            } catch (e) { message.error(e.message); }
        }
        async function provision(row) {
            provisioning.value = row.id;
            try {
                await api.post('/api/grafana/' + row.id + '/provision');
                message.success('告警规则已同步到 Grafana');
            } catch (e) { message.error(e.message || '同步失败'); }
            provisioning.value = null;
        }
        async function cleanupRules(row) {
            try {
                const res = await api.post('/api/grafana/' + row.id + '/cleanup-rules');
                message.success('已清理 ' + (res.deleted || 0) + ' 条告警规则');
            } catch (e) { message.error(e.message || '清理失败'); }
        }

        const columns = useColumns([
            { title: '名称', key: 'name', width: 120 },
            { title: 'Grafana URL', key: 'grafana_url', ellipsis: { tooltip: true }, _hideOnMobile: true },
            { title: '数据源 UID', key: 'datasource_uid', width: 120, ellipsis: { tooltip: true }, _hideOnMobile: true },
            { title: '状态', key: 'status', width: 100, render: row => h(NSpace, { size: 4 }, () => [
                h(NTag, { size: 'small', type: row.enabled ? 'success' : 'default' }, () => row.enabled ? '启用' : '禁用'),
                row.running ? h(NBadge, { dot: true, type: 'success' }) : null,
            ])},
            { title: '操作', key: 'actions', width: _isMobile.value ? 260 : 400, render: row => h(NSpace, { size: 'small' }, () => [
                h(NButton, { size: 'tiny', secondary: true, onClick: () => openEdit(row) }, () => '编辑'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => toggle(row) }, () => row.enabled ? '禁用' : '启用'),
                h(NButton, { size: 'tiny', secondary: true, onClick: () => test(row) }, () => '测试'),
                h(NButton, { size: 'tiny', type: 'info', secondary: true, loading: provisioning.value === row.id, onClick: () => provision(row) }, () => '同步规则'),
                h(NPopconfirm, { onPositiveClick: () => cleanupRules(row) }, { trigger: () => h(NButton, { size: 'tiny', type: 'warning', secondary: true }, () => '清理规则'), default: () => '确认清理该配置在Grafana中的所有告警规则？' }),
                h(NPopconfirm, { onPositiveClick: () => del(row) }, { trigger: () => h(NButton, { size: 'tiny', type: 'error', secondary: true }, () => '删除'), default: () => '确认删除？' }),
            ])},
        ]);

        const gridCols = computed(() => _isMobile.value ? 1 : 2);
        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('h3', { class: 'page-title' }, 'Grafana 告警配置'),
                h(NButton, { type: 'primary', size: 'small', onClick: openAdd }, () => '+ 新增'),
            ]),
            h(NDataTable, { columns: columns.value, data: configs.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 500 : undefined }),
            h(NModal, { show: showModal.value, onUpdateShow: v => showModal.value = v, preset: 'card', title: editingId.value ? '编辑 Grafana 配置' : '新增 Grafana 配置', style: _isMobile.value ? 'width:95vw' : 'width:680px' }, () =>
                h(NGrid, { cols: gridCols.value, xGap: 12 }, () => [
                    h(NGi, null, () => h(NFormItem, { label: '名称', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.name, onUpdateValue: v => form.name = v, placeholder: '如: 生产环境 Grafana' }))),
                    h(NGi, null, () => h(NFormItem, { label: 'Grafana URL', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.grafana_url, onUpdateValue: v => form.grafana_url = v, placeholder: 'http://grafana:3000' }))),
                    h(NGi, null, () => h(NFormItem, { label: '用户名', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.username, onUpdateValue: v => form.username = v, placeholder: 'admin' }))),
                    h(NGi, null, () => h(NFormItem, { label: '密码', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.password, onUpdateValue: v => form.password = v, type: 'password', showPasswordOn: 'click', placeholder: editingId.value ? '留空不修改' : 'Grafana 密码' }))),
                    h(NGi, { span: gridCols.value }, () => h(NFormItem, { label: '数据源 UID', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NSpace, { align: 'center', size: 'small', wrap: false, style: 'width:100%' }, () => [
                        h(NSelect, { value: form.datasource_uid, onUpdateValue: v => form.datasource_uid = v, options: datasources.value, filterable: true, tag: true, placeholder: '选择或输入数据源', style: 'flex:1;min-width:0', loading: dsLoading.value }),
                        h(NButton, { size: 'small', secondary: true, loading: dsLoading.value, onClick: fetchDatasources }, () => '获取'),
                    ]))),
                    h(NGi, null, () => h(NFormItem, { label: 'Webhook URL', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInput, { value: form.webhook_url, onUpdateValue: v => form.webhook_url = v, placeholder: '本系统公网地址' }))),
                    h(NGi, null, () => h(NFormItem, { label: 'Webhook 密钥', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NSpace, { align: 'center', size: 'small', wrap: false }, () => [
                        h(NInput, { value: form.webhook_secret, onUpdateValue: v => form.webhook_secret = v, placeholder: '自动生成', style: 'flex:1;min-width:0', readonly: true }),
                        h(NButton, { size: 'small', secondary: true, onClick: async () => { try { const res = await api.post('/api/grafana/generate-secret'); form.webhook_secret = res.secret; } catch {} } }, () => '重新生成'),
                    ]))),
                    h(NGi, { span: gridCols.value }, () => h(NFormItem, { label: '告警规则', labelPlacement: 'top' }, () => h(NSelect, { value: form.auto_rules, onUpdateValue: v => form.auto_rules = v, options: ruleOptions.value, multiple: true, placeholder: '选择要自动创建的告警规则' }))),
                    h(NGi, null, () => h(NFormItem, { label: '检查间隔(秒)', labelPlacement: _isMobile.value ? 'top' : 'left' }, () => h(NInputNumber, { value: form.interval_sec, onUpdateValue: v => form.interval_sec = v, min: 10 }))),
                    h(NGi, { span: gridCols.value }, () => h(NButton, { type: 'primary', block: true, loading: saving.value, onClick: save }, () => '保存')),
                ])
            ),
        ]);
    }
});

// --- Grafana Alerts ---
const GrafanaAlertsPage = defineComponent({
    setup() {
        const alerts = ref([]);
        const total = ref(0);
        const page = ref(1);
        const pageSize = 20;
        const loading = ref(true);
        const { connected, messages, stop } = useWebSocket('/ws/grafana-logs');
        onUnmounted(stop);

        async function load() {
            loading.value = true;
            try {
                const res = await api.get('/api/grafana/alerts?page=' + page.value + '&page_size=' + pageSize);
                alerts.value = res.data || [];
                total.value = res.total || 0;
            } catch {}
            loading.value = false;
        }
        onMounted(load);
        watch(page, load);

        watch(() => messages.value.length, () => {
            const latest = messages.value[messages.value.length - 1];
            if (latest && latest.type === 'grafana_alert' && page.value === 1) {
                load();
            }
        });

        const statusType = (s) => {
            const map = { firing: 'error', resolved: 'success' };
            return map[s] || 'default';
        };

        const columns = useColumns([
            { title: '时间', key: 'detected_at', width: 150, render: row => h('span', { style: 'font-size:12px;opacity:0.65' }, formatTime(row.detected_at)) },
            { title: '配置', key: 'config_name', width: 100 },
            { title: '告警名称', key: 'alert_name', width: 150, ellipsis: { tooltip: true } },
            { title: '状态', key: 'status', width: 80, render: row => h(NTag, { type: statusType(row.status), size: 'small', bordered: false }, () => row.status) },
            { title: '严重度', key: 'severity', width: 80, _hideOnMobile: true, render: row => row.severity ? h(NTag, { size: 'small', bordered: false }, () => row.severity) : '-' },
            { title: '摘要', key: 'summary', ellipsis: { tooltip: true }, _hideOnMobile: true },
        ]);

        return () => h('div', { class: 'page-body' }, [
            h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:16px' }, [
                h('div', { style: 'display:flex;align-items:center;gap:12px' }, [
                    h('h3', { class: 'page-title' }, 'Grafana 告警记录'),
                    h('div', { style: 'display:flex;align-items:center;gap:4px;font-size:12px;opacity:0.5' }, [
                        h('span', { class: connected.value ? 'ws-dot connected' : 'ws-dot disconnected' }),
                        connected.value ? '实时' : '离线'
                    ]),
                ]),
            ]),
            h(NDataTable, { columns: columns.value, data: alerts.value, bordered: false, size: 'small', loading: loading.value, maxHeight: 'calc(100vh - 200px)', scrollX: _isMobile.value ? 400 : undefined }),
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
            { label: 'Grafana', key: 'g-grafana' },
            { label: '监控日志', key: 'monitor-logs' },
            { label: '系统', key: 'g-system' },
        ];

        const groupTabs = {
            'g-mysql': [
                { label: '数据库', key: 'databases' },
                { label: '慢SQL', key: 'slow-queries' },
                { label: '已忽略SQL', key: 'ignored-sql' },
                { label: '自定义SQL', key: 'custom-sql' },
                { label: 'SQL结果', key: 'custom-sql-logs' },
            ],
            'g-rocketmq': [
                { label: 'MQ 配置', key: 'rocketmq' },
                { label: 'MQ 告警', key: 'rocketmq-alerts' },
            ],
            'g-grafana': [
                { label: '告警配置', key: 'grafana' },
                { label: '告警日志', key: 'grafana-alerts' },
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
            const topbarH = '60px';
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
    { path: '/ignored-sql', component: IgnoredSQLPage },
    { path: '/custom-sql', component: CustomSQLPage },
    { path: '/custom-sql-logs', component: CustomSQLLogsPage },
    { path: '/monitor-logs', component: MonitorLogsPage },
    { path: '/rocketmq', component: RocketMQPage },
    { path: '/rocketmq-alerts', component: RocketMQAlertsPage },
    { path: '/health-checks', component: HealthChecksPage },
    { path: '/health-checks-logs', component: HealthCheckLogsPage },
    { path: '/grafana', component: GrafanaPage },
    { path: '/grafana-alerts', component: GrafanaAlertsPage },
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
