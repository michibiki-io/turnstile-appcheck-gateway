<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from '@iconify/svelte';
  import { Alert, Badge, Button } from 'flowbite-svelte';
  import { Doughnut, Line } from 'svelte-chartjs';
  import {
    ArcElement,
    CategoryScale,
    Chart as ChartJS,
    Filler,
    Legend,
    LineElement,
    LinearScale,
    PointElement,
    type Plugin,
    TimeScale,
    Tooltip
  } from 'chart.js';
  import type { AuditEvent, AuditFilters, AuditOptions, AuditPage, AdminMe, MetricsResponse, Summary } from './api';
  import { ApiError, apiGet, apiGetWithAuthRecovery, apiPost, auditParams, isTransientAuthStatus, rangeToParams } from './api';
  import Detail from './components/Detail.svelte';
  import Field from './components/Field.svelte';
  import { resolvedTheme, setThemeMode, themePreference, type ThemeMode } from './theme';

  const centerTextPlugin = {
    id: 'centerText',
    afterDraw(chart) {
      if ((chart.config as { type?: string }).type !== 'doughnut') return;
      const dataset = chart.data.datasets[0];
      const values = (dataset?.data ?? []).map((value) => Number(value) || 0);
      const total = values.reduce((sum, value) => sum + value, 0);
      if (total <= 0) return;

      const { ctx, chartArea } = chart;
      const centerX = (chartArea.left + chartArea.right) / 2;
      const centerY = (chartArea.top + chartArea.bottom) / 2;
      const fontFamily = 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';

      ctx.save();
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      const dark = document.documentElement.classList.contains('dark');
      ctx.fillStyle = dark ? '#CBD5E1' : '#475569';
      ctx.font = `600 12px ${fontFamily}`;
      ctx.fillText('Total', centerX, centerY - 9);
      ctx.fillStyle = dark ? '#F8FAFC' : '#0f172a';
      ctx.font = `700 17px ${fontFamily}`;
      ctx.fillText(total.toLocaleString(), centerX, centerY + 12);
      ctx.restore();
    }
  } satisfies Plugin<'doughnut'>;

  ChartJS.register(CategoryScale, LinearScale, LineElement, PointElement, TimeScale, Tooltip, Filler, ArcElement, Legend, centerTextPlugin);

  const ranges = [
    { value: '1h', label: '1 hour' },
    { value: '6h', label: '6 hours' },
    { value: '24h', label: '24 hours' },
    { value: '7d', label: '7 days' },
    { value: '30d', label: '1 month' }
  ];

  const themeModes: Array<{ value: ThemeMode; label: string; icon: string }> = [
    { value: 'system', label: 'System', icon: 'mdi:monitor' },
    { value: 'light', label: 'Light', icon: 'mdi:white-balance-sunny' },
    { value: 'dark', label: 'Dark', icon: 'mdi:moon-waning-crescent' }
  ];

  const sidebarStorageKey = 'turnstile-appcheck-gateway.sidebarCollapsed';
  const appName = 'Turnstile App Check Gateway';

  type ActiveView = 'dashboard' | 'audit';
  type PaginationItem = number | 'ellipsis';

  let me: AdminMe | null = null;
  let metrics: MetricsResponse | null = null;
  let auditOptions: AuditOptions = { actions: [], endpoints: [], results: [] };
  let page: AuditPage = { items: [], total: 0, nextCursor: null, hasNext: false };
  let activeView: ActiveView = 'dashboard';
  let selected: AuditEvent | null = null;
  let detailOpen = false;
  let resetOpen = false;
  let resetConfirmation = '';
  let resetReason = '';
  let resetting = false;
  let loading = true;
  let metricsLoading = false;
  let auditLoading = false;
  let error = '';
  let recoverableAuthError = false;
  let pageIndex = 0;
  let cursor = '';
  let cursorStack: string[] = [];
  let metricsRange = '24h';
  let sidebarCollapsed = initialSidebarCollapsed();
  let mobileMenuOpen = false;
  let themeMenuOpen = false;
  let filters: AuditFilters = {
    from: '',
    to: '',
    pageSize: '25',
    actor: '',
    action: '',
    endpoint: '',
    path: '',
    method: '',
    result: '',
    statusCode: '',
    requestId: ''
  };
  $: pageSize = Number(filters.pageSize) > 0 ? Math.min(Number(filters.pageSize), 200) : 25;
  $: currentPageNumber = pageIndex + 1;
  $: knownPageCount = pageIndex + 1 + (page.hasNext && page.nextCursor ? 1 : 0);
  $: paginationItems = buildPaginationItems(currentPageNumber, knownPageCount);

  $: isDarkTheme = $resolvedTheme === 'dark';
  $: brandIcon = isDarkTheme ? './turnstile-appcheck-gateway-logo-dark.svg' : './turnstile-appcheck-gateway-logo.svg';
  $: brandWordmark = isDarkTheme ? './turnstile-appcheck-gateway-logo-str-dark.svg' : './turnstile-appcheck-gateway-logo-str.svg';
  $: pageTitle = activeView === 'audit' ? 'Audit Log' : 'Dashboard';
  $: pageDescription = activeView === 'audit' ? 'Search, inspect, and reset gateway audit events.' : 'Monitor gateway request health and traffic trends.';
  $: chartTextColor = isDarkTheme ? '#CBD5E1' : '#475569';
  $: chartGridColor = isDarkTheme ? 'rgba(148, 163, 184, 0.24)' : 'rgba(203, 213, 225, 0.7)';
  $: chartSurfaceColor = isDarkTheme ? '#0F172A' : '#FFFFFF';
  $: selectedThemeLabel = themeModes.find((mode) => mode.value === $themePreference)?.label ?? 'System';
  $: summary = metrics?.summary ?? emptySummary();
  $: publicApiValues = [summary.successful, summary.failed];
  $: exchangeValues = [summary.exchangeSuccesses, summary.exchangeFailures];
  $: verifyValues = [summary.verifySuccesses, summary.verifyFailures];
  $: statusClassValues = [summary.count4xx, summary.count5xx];
  $: publicApiData = doughnutData(['gateway success', 'gateway failure'], publicApiValues, ['#16a34a', '#dc2626']);
  $: exchangeData = doughnutData(['exchange success', 'exchange failure'], exchangeValues, ['#0f766e', '#ea580c']);
  $: verifyData = doughnutData(['verify success', 'verify failure'], verifyValues, ['#2563eb', '#f97316']);
  $: statusClassData = doughnutData(['4xx', '5xx'], statusClassValues, ['#f59e0b', '#7f1d1d']);
  $: chartData = {
    labels: metrics?.points.map((point) => new Date(point.timestamp).toLocaleString()) ?? [],
    datasets: [
      {
        label: 'Gateway requests',
        data: metrics?.points.map((point) => point.count) ?? [],
        borderColor: isDarkTheme ? '#38BDF8' : '#2563eb',
        backgroundColor: isDarkTheme ? 'rgba(56, 189, 248, 0.18)' : 'rgba(37, 99, 235, 0.12)',
        pointRadius: 2,
        tension: 0.25,
        fill: true
      }
    ]
  };
  $: chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: { mode: 'index' as const, intersect: false }
    },
    scales: {
      x: { grid: { color: chartGridColor }, ticks: { color: chartTextColor, maxRotation: 0, autoSkip: true, maxTicksLimit: 8 } },
      y: { grid: { color: chartGridColor }, beginAtZero: true, ticks: { color: chartTextColor, precision: 0 } }
    }
  };
  $: doughnutOptions = {
    responsive: true,
    maintainAspectRatio: false,
    cutout: '62%',
    plugins: {
      legend: {
        position: 'bottom' as const,
        labels: { boxWidth: 10, color: chartTextColor, usePointStyle: true }
      }
    },
    elements: {
      arc: { borderColor: chartSurfaceColor }
    }
  };

  onMount(() => {
    syncViewFromHash();
    window.addEventListener('hashchange', syncViewFromHash);
    window.addEventListener('keydown', handleGlobalKeydown);
    reloadAll();
    return () => {
      window.removeEventListener('hashchange', syncViewFromHash);
      window.removeEventListener('keydown', handleGlobalKeydown);
    };
  });

  async function reloadAll() {
    loading = true;
    error = '';
    recoverableAuthError = false;
    try {
      me = await apiGetWithAuthRecovery<AdminMe>('/me');
      auditOptions = await apiGetWithAuthRecovery<AuditOptions>('/audit-options');
      await loadMetrics(true);
      await loadAudit('', 0, [], true);
    } catch (err) {
      if (isRecoverableAuthError(err)) {
        recoverableAuthError = true;
        error = 'Authentication is not ready or admin access was denied. Refresh authentication and try again.';
      } else {
        error = err instanceof Error ? err.message : 'Failed to load dashboard data';
      }
    } finally {
      loading = false;
    }
  }

  async function loadMetrics(withAuthRecovery = false) {
    metricsLoading = true;
    try {
      const params = rangeToParams(metricsRange);
      metrics = withAuthRecovery
        ? await apiGetWithAuthRecovery<MetricsResponse>('/request-metrics', params)
        : await apiGet<MetricsResponse>('/request-metrics', params);
    } finally {
      metricsLoading = false;
    }
  }

  async function loadAudit(nextCursor: string, nextPageIndex: number, nextCursorStack = cursorStack, withAuthRecovery = false) {
    auditLoading = true;
    try {
      cursor = nextCursor;
      pageIndex = Math.max(0, nextPageIndex);
      cursorStack = nextCursorStack;
      const params = auditParams(filters, pageSize, cursor);
      page = withAuthRecovery ? await apiGetWithAuthRecovery<AuditPage>('/audit-events', params) : await apiGet<AuditPage>('/audit-events', params);
    } finally {
      auditLoading = false;
    }
  }

  async function loadNextAuditPage() {
    if (!page.nextCursor || auditLoading) return;
    await loadAudit(page.nextCursor, pageIndex + 1, [...cursorStack, cursor]);
  }

  async function loadPreviousAuditPage() {
    if (pageIndex === 0 || auditLoading) return;
    await loadAuditPageNumber(pageIndex);
  }

  async function loadAuditPageNumber(pageNumber: number) {
    if (auditLoading) return;
    const nextPageIndex = Math.min(Math.max(pageNumber - 1, 0), knownPageCount - 1);
    if (nextPageIndex === pageIndex) return;

    if (nextPageIndex === pageIndex + 1 && page.nextCursor) {
      await loadAudit(page.nextCursor, nextPageIndex, [...cursorStack, cursor]);
      return;
    }

    const nextCursor = cursorForPageIndex(nextPageIndex);
    await loadAudit(nextCursor, nextPageIndex, cursorStack.slice(0, nextPageIndex));
  }

  async function applyFilters() {
    error = '';
    recoverableAuthError = false;
    try {
      await loadAudit('', 0, []);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to apply filters';
    }
  }

  function emptySummary(): Summary {
    return {
      total: 0,
      successful: 0,
      failed: 0,
      exchangeSuccesses: 0,
      exchangeFailures: 0,
      verifySuccesses: 0,
      verifyFailures: 0,
      count4xx: 0,
      count5xx: 0
    };
  }

  function resultColor(result: string) {
    if (result === 'success') return 'green';
    if (result === 'denied') return 'yellow';
    return 'red';
  }

  function fmtTime(value: string) {
    return value ? new Date(value).toLocaleString() : '';
  }

  function fmtEventTime(item: AuditEvent | null) {
    if (!item) return '';
    return item.timestampDisplay || fmtTime(item.timestamp);
  }

  function eventTarget(item: AuditEvent) {
    return item.endpoint || item.path || '-';
  }

  function eventMessage(item: AuditEvent) {
    return item.message || item.errorCode || '-';
  }

  function showDetail(item: AuditEvent) {
    selected = item;
    detailOpen = true;
  }

  function syncViewFromHash() {
    activeView = window.location.hash === '#audit-log' ? 'audit' : 'dashboard';
  }

  function setView(view: ActiveView) {
    activeView = view;
    mobileMenuOpen = false;
    window.location.hash = view === 'audit' ? 'audit-log' : 'dashboard';
  }

  function initialSidebarCollapsed(): boolean {
    if (typeof window === 'undefined') return false;
    try {
      return window.localStorage.getItem(sidebarStorageKey) === 'true';
    } catch {
      return false;
    }
  }

  function setSidebarCollapsed(next: boolean) {
    sidebarCollapsed = next;
    if (typeof window !== 'undefined') {
      try {
        window.localStorage.setItem(sidebarStorageKey, String(next));
      } catch {
        // Storage may be disabled by browser policy. Keep the in-memory state.
      }
    }
  }

  function toggleThemeMenu() {
    themeMenuOpen = !themeMenuOpen;
    if (themeMenuOpen) mobileMenuOpen = false;
  }

  function chooseTheme(mode: ThemeMode) {
    setThemeMode(mode);
    themeMenuOpen = false;
  }

  function handleGlobalKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return;
    themeMenuOpen = false;
    mobileMenuOpen = false;
    if (detailOpen) detailOpen = false;
    if (resetOpen && !resetting) resetOpen = false;
  }

  async function changePageSize() {
    await loadAudit('', 0, []);
  }

  function buildPaginationItems(current: number, knownTotal: number): PaginationItem[] {
    if (knownTotal <= 7) return Array.from({ length: knownTotal }, (_, index) => index + 1);

    const items: PaginationItem[] = [1];
    let start = Math.max(2, current - 1);
    let end = Math.min(knownTotal - 1, current + 1);

    if (current <= 3) {
      start = 2;
      end = 4;
    } else if (current >= knownTotal - 2) {
      start = knownTotal - 3;
      end = knownTotal - 1;
    }

    if (start > 2) items.push('ellipsis');
    for (let pageNumber = start; pageNumber <= end; pageNumber += 1) {
      items.push(pageNumber);
    }
    if (end < knownTotal - 1) items.push('ellipsis');
    items.push(knownTotal);
    return items;
  }

  function cursorForPageIndex(targetPageIndex: number) {
    if (targetPageIndex <= 0) return '';
    if (targetPageIndex === pageIndex) return cursor;
    return cursorStack[targetPageIndex] ?? '';
  }

  function resetFilters() {
    filters = { ...filters, from: '', to: '', pageSize: '25', actor: '', action: '', endpoint: '', path: '', method: '', result: '', statusCode: '', requestId: '' };
  }

  function refreshAuthentication() {
    window.location.reload();
  }

  function isRecoverableAuthError(err: unknown) {
    return err instanceof ApiError && isTransientAuthStatus(err.status);
  }

  function doughnutData(labels: string[], values: number[], colors: string[]) {
    return {
      labels,
      datasets: [
        {
          data: values,
          backgroundColor: colors,
          borderWidth: 0
        }
      ]
    };
  }

  function hasCounts(values: number[]) {
    return values.some((value) => value > 0);
  }

  function openResetModal() {
    resetConfirmation = '';
    resetReason = '';
    resetOpen = true;
  }

  async function resetAuditLog() {
    resetting = true;
    error = '';
    try {
      await apiPost<{ status: string }>('/audit-events/reset', {
        confirmation: resetConfirmation,
        reason: resetReason
      });
      resetOpen = false;
      resetFilters();
      await reloadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to reset audit log';
    } finally {
      resetting = false;
    }
  }
</script>

<main class={`admin-shell ${sidebarCollapsed ? 'sidebar-collapsed' : ''}`}>
  <aside class="admin-sidebar" aria-label="Primary navigation">
    <div class="admin-brand">
      {#if sidebarCollapsed}
        <img src={brandIcon} alt={appName} class="h-10 w-10" />
      {:else}
        <div class="flex w-fit items-start gap-1.5">
          <img src={brandWordmark} alt={appName} class="h-10 w-auto max-w-36" />
          {#if me}
            <span class="version-badge">{me.version}</span>
          {/if}
        </div>
      {/if}
    </div>

    <nav class="admin-nav">
      <button
        class:active={activeView === 'dashboard'}
        class="admin-nav-item"
        aria-current={activeView === 'dashboard' ? 'page' : undefined}
        aria-label="Dashboard"
        title="Dashboard"
        onclick={() => setView('dashboard')}
      >
        <Icon icon="lets-icons:chart" class="h-5 w-5" />
        {#if !sidebarCollapsed}<span>Dashboard</span>{/if}
      </button>
      <button
        class:active={activeView === 'audit'}
        class="admin-nav-item"
        aria-current={activeView === 'audit' ? 'page' : undefined}
        aria-label="Audit Log"
        title="Audit Log"
        onclick={() => setView('audit')}
      >
        <Icon icon="lets-icons:order" class="h-5 w-5" />
        {#if !sidebarCollapsed}<span>Audit Log</span>{/if}
      </button>
    </nav>

    <div class="admin-sidebar-collapse">
      <button
        class="icon-button sidebar-toggle-button"
        aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        title={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        aria-pressed={sidebarCollapsed}
        onclick={() => setSidebarCollapsed(!sidebarCollapsed)}
      >
        <Icon icon={sidebarCollapsed ? 'lucide:panel-left-open' : 'lucide:panel-left-close'} class="h-5 w-5" />
      </button>
    </div>

    <div class="admin-sidebar-footer">
      {#if me && !sidebarCollapsed}
        {#if me.commitURL}
          <a class="inline-flex max-w-full items-center gap-1.5 truncate hover:underline" href={me.commitURL} target="_blank" rel="noreferrer">
            <Icon icon="mdi:github" class="h-4 w-4 shrink-0" />
            <span class="truncate">{me.shortCommit}</span>
          </a>
        {:else}
          <span class="truncate">Commit {me.shortCommit}</span>
        {/if}
      {/if}
    </div>
  </aside>

  <div class="admin-content">
    <header class="admin-topbar">
      <div class="flex min-w-0 items-center gap-3">
        <button
          class="icon-button mobile-menu-button"
          aria-label="Open navigation menu"
          aria-haspopup="menu"
          aria-expanded={mobileMenuOpen}
          title="Menu"
          onclick={() => {
            mobileMenuOpen = true;
            themeMenuOpen = false;
          }}
        >
          <Icon icon="mdi:menu" class="h-5 w-5" />
        </button>
        <div class="min-w-0">
          <h1 class="truncate text-xl font-semibold tracking-normal md:text-2xl">{pageTitle}</h1>
          <p class="hidden truncate text-sm md:block">{pageDescription}</p>
        </div>
      </div>

      <div class="topbar-actions">
        <div class="relative">
          <button
            class="icon-button"
            aria-haspopup="menu"
            aria-expanded={themeMenuOpen}
            aria-label="Select theme"
            title={`Theme: ${selectedThemeLabel}`}
            onclick={toggleThemeMenu}
          >
            <Icon icon={isDarkTheme ? 'mdi:moon-waning-crescent' : 'mdi:white-balance-sunny'} class="h-5 w-5" />
          </button>
          {#if themeMenuOpen}
            <div class="theme-menu" role="menu" aria-label="Theme">
              {#each themeModes as mode}
                <button
                  class:active={$themePreference === mode.value}
                  role="menuitemradio"
                  aria-checked={$themePreference === mode.value}
                  onclick={() => chooseTheme(mode.value)}
                >
                  <Icon icon={mode.icon} class="h-4 w-4" />
                  <span>{mode.label}</span>
                  {#if $themePreference === mode.value}
                    <Icon icon="mdi:check" class="ml-auto h-4 w-4" />
                  {/if}
                </button>
              {/each}
            </div>
          {/if}
        </div>

        {#if me}
          <div class="user-chip">
            <span class="user-avatar" aria-hidden="true">
              <Icon icon="lets-icons:user" class="h-5 w-5" />
            </span>
            <span class="max-w-[11rem] truncate text-sm font-semibold">{me.user || me.email || 'anonymous'}</span>
          </div>
        {/if}
      </div>
    </header>

    {#if mobileMenuOpen}
      <div class="mobile-menu-backdrop" role="presentation">
        <div class="mobile-menu-panel">
          <div class="flex items-center justify-between border-b border-[var(--border-color)] px-4 py-4">
            <div class="flex min-w-0 items-center gap-3">
              <img src={brandIcon} alt="" class="h-10 w-10 shrink-0" />
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold">{appName}</div>
                {#if me}<div class="mt-0.5 text-xs text-[var(--text-secondary)]">{me.version}</div>{/if}
              </div>
            </div>
            <button class="icon-button" aria-label="Close navigation menu" title="Close" onclick={() => (mobileMenuOpen = false)}>
              <Icon icon="lets-icons:close-round" class="h-5 w-5" />
            </button>
          </div>
          <div class="grid gap-2 p-3" role="menu" aria-label="Navigation">
            <button class:active={activeView === 'dashboard'} class="admin-nav-item" role="menuitem" onclick={() => setView('dashboard')}>
              <Icon icon="lets-icons:chart" class="h-5 w-5" />
              <span>Dashboard</span>
            </button>
            <button class:active={activeView === 'audit'} class="admin-nav-item" role="menuitem" onclick={() => setView('audit')}>
              <Icon icon="lets-icons:order" class="h-5 w-5" />
              <span>Audit Log</span>
            </button>
          </div>
        </div>
      </div>
    {/if}

      <div class="flex-1 px-4 py-5 sm:px-6 lg:px-8">
        <div class="mx-auto flex w-full max-w-7xl flex-col gap-5">
          {#if me?.authDisabled}
            <Alert color="yellow">
              <div class="flex items-center gap-2">
                <Icon icon="lets-icons:warning" class="h-5 w-5" />
                <span>Authentication is disabled. Protect this dashboard with an upstream access-control layer.</span>
              </div>
            </Alert>
          {/if}

          {#if error}
            <Alert color="red">
              <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <span>{error}</span>
                {#if recoverableAuthError}
                  <Button color="red" size="sm" onclick={refreshAuthentication}>Refresh authentication</Button>
                {/if}
              </div>
            </Alert>
          {/if}

          {#if activeView === 'dashboard'}
            <section class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <div class="chart-card">
                <h2 class="chart-card-title">Public gateway request result</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(publicApiValues)}
                    <Doughnut data={publicApiData} options={doughnutOptions} />
                  {:else}
                    <div class="empty-doughnut">No data</div>
                  {/if}
                </div>
              </div>
              <div class="chart-card">
                <h2 class="chart-card-title">Exchange request result</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(exchangeValues)}
                    <Doughnut data={exchangeData} options={doughnutOptions} />
                  {:else}
                    <div class="empty-doughnut">No data</div>
                  {/if}
                </div>
              </div>
              <div class="chart-card">
                <h2 class="chart-card-title">Verify request result</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(verifyValues)}
                    <Doughnut data={verifyData} options={doughnutOptions} />
                  {:else}
                    <div class="empty-doughnut">No data</div>
                  {/if}
                </div>
              </div>
              <div class="chart-card">
                <h2 class="chart-card-title">Error status class</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(statusClassValues)}
                    <Doughnut data={statusClassData} options={doughnutOptions} />
                  {:else}
                    <div class="empty-doughnut">No data</div>
                  {/if}
                </div>
              </div>
            </section>

            <section class="panel p-5">
              <div class="mb-4 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div>
                  <h2 class="text-lg font-semibold">Request trend</h2>
                  <p class="text-sm text-[var(--text-secondary)]">Request volume over the selected time range</p>
                </div>
                <div class="flex flex-wrap gap-2">
                  {#each ranges as range}
                    <button class:active={metricsRange === range.value} class="secondary-button range-button" type="button" disabled={metricsLoading} aria-busy={metricsLoading && metricsRange === range.value} onclick={async () => { metricsRange = range.value; await loadMetrics(); }}>
                      {range.label}
                    </button>
                  {/each}
                </div>
              </div>
              <div class="h-72">
                {#if metricsLoading}
                  <div class="flex h-full items-center justify-center text-sm text-[var(--text-secondary)]" role="status" aria-live="polite">Loading chart</div>
                {:else}
                  <Line data={chartData} options={chartOptions} />
                {/if}
              </div>
            </section>
          {:else}
            <section class="panel overflow-hidden">
              <div class="flex items-center justify-end border-b border-[var(--border-color)] px-5 py-4">
                <div class="flex flex-wrap justify-end gap-2">
                  <button class="secondary-button" type="button" disabled={loading || auditLoading} aria-busy={loading || auditLoading} onclick={reloadAll}>Refresh</button>
                  <Button color="red" onclick={openResetModal}>Reset</Button>
                </div>
              </div>

      <div class="border-b border-[var(--border-color)] p-5">
        <fieldset class="contents" disabled={auditLoading} aria-busy={auditLoading}>
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Field label="Actor"><input class="admin-input" placeholder=" " bind:value={filters.actor} autocomplete="off" /></Field>
          <Field label="Action">
            <select class="admin-input" bind:value={filters.action}>
              <option value="">Any</option>
              {#each auditOptions.actions as action}
                <option value={action}>{action}</option>
              {/each}
            </select>
          </Field>
          <Field label="Endpoint">
            <select class="admin-input" bind:value={filters.endpoint}>
              <option value="">Any</option>
              {#each auditOptions.endpoints as endpoint}
                <option value={endpoint}>{endpoint}</option>
              {/each}
            </select>
          </Field>
          <Field label="Result">
            <select class="admin-input" bind:value={filters.result}>
              <option value="">Any</option>
              {#each auditOptions.results as result}
                <option value={result}>{result}</option>
              {/each}
            </select>
          </Field>
          <Field label="From"><input class="admin-input" type="datetime-local" placeholder=" " bind:value={filters.from} /></Field>
          <Field label="To"><input class="admin-input" type="datetime-local" placeholder=" " bind:value={filters.to} /></Field>
          <Field label="Status code"><input class="admin-input" type="number" min="100" max="599" step="1" placeholder=" " bind:value={filters.statusCode} /></Field>
          <Field label="Method">
            <select class="admin-input" bind:value={filters.method}>
              <option value="">Any</option>
              <option>GET</option>
              <option>POST</option>
              <option>OPTIONS</option>
            </select>
          </Field>
        </div>
        <div class="mt-4 flex flex-wrap items-end gap-4">
          <Field label="Request ID"><input class="admin-input min-w-72" placeholder=" " bind:value={filters.requestId} autocomplete="off" /></Field>
          <Field label="Path"><input class="admin-input min-w-72" placeholder=" " bind:value={filters.path} autocomplete="off" /></Field>
        </div>
        <div class="mt-5 flex flex-wrap items-center gap-2 border-t border-[var(--border-color)] pt-4">
          <Button color="blue" disabled={auditLoading} aria-busy={auditLoading} onclick={applyFilters}>{auditLoading ? 'Applying' : 'Apply'}</Button>
          <button class="secondary-button" type="button" onclick={async () => { resetFilters(); await applyFilters(); }}>Clear filters</button>
          <span class="ml-auto text-sm text-[var(--text-secondary)]" aria-live="polite">{auditLoading ? 'Loading events' : `${page.total} events match the current filters`}</span>
        </div>
        </fieldset>
      </div>

      <div class="hidden px-5 pt-5 md:block">
        <div class="overflow-x-auto pb-3">
          <table class="data-table">
            <thead>
              <tr>
                <th class="px-4 py-3 font-semibold">Timestamp</th>
                <th class="px-4 py-3 font-semibold">Actor</th>
                <th class="px-4 py-3 font-semibold">Action</th>
                <th class="px-4 py-3 font-semibold">Target</th>
                <th class="px-4 py-3 font-semibold">Result</th>
                <th class="px-4 py-3 font-semibold">Remote</th>
                <th class="px-4 py-3 font-semibold">Message</th>
                <th class="px-4 py-3 font-semibold"></th>
              </tr>
            </thead>
            <tbody>
              {#each page.items as item}
                <tr>
                  <td class="whitespace-nowrap px-4 py-4">{fmtEventTime(item)}</td>
                  <td class="px-4 py-4">{item.actor}</td>
                  <td class="px-4 py-4">
                    <Badge color="blue">{item.action}</Badge>
                  </td>
                  <td class="max-w-56 truncate px-4 py-4">{eventTarget(item)}</td>
                  <td class="px-4 py-3"><Badge color={resultColor(item.result)}>{item.result}</Badge></td>
                  <td class="max-w-48 truncate px-4 py-4">{item.remoteAddr}</td>
                  <td class="max-w-72 px-4 py-4">{eventMessage(item)}</td>
                  <td class="px-4 py-4 text-right">
                    <button class="icon-action-button" type="button" aria-label="Show event details" title="Show details" onclick={() => showDetail(item)}>
                      <Icon icon="lets-icons:view" class="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              {:else}
                <tr>
                  <td colspan={8} class="px-4 py-8 text-center text-[var(--text-secondary)]">No audit events</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>

      <div class="space-y-4 px-4 pb-3 md:hidden">
        {#each page.items as item}
          <article class="mobile-card">
            <div class="text-sm font-medium">{fmtEventTime(item)}</div>
            <div class="mt-2 break-words text-sm font-semibold">{item.actor}</div>
            <div class="mt-4 flex flex-wrap gap-2">
              <Badge color="blue">{item.action}</Badge>
              <Badge color={resultColor(item.result)}>{item.result}</Badge>
            </div>
            <p class="mt-4 break-words text-sm">{eventMessage(item)}</p>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div>
                <dt class="text-xs font-medium text-[var(--text-secondary)]">Target</dt>
                <dd class="mt-1 break-words">{eventTarget(item)}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium text-[var(--text-secondary)]">Status</dt>
                <dd class="mt-1">{item.statusCode || '-'}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium text-[var(--text-secondary)]">Remote</dt>
                <dd class="mt-1 break-words">{item.remoteAddr || '-'}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium text-[var(--text-secondary)]">Duration</dt>
                <dd class="mt-1">{item.durationMs} ms</dd>
              </div>
            </dl>
            <button class="secondary-button mt-4" type="button" onclick={() => showDetail(item)}>Show details</button>
          </article>
        {:else}
          <div class="mobile-card px-4 py-8 text-center text-sm text-[var(--text-secondary)]">No audit events</div>
        {/each}
      </div>

      <nav class="pagination-bar" aria-label="Pagination">
        <div class="pagination-status" aria-live="polite">
          <span>Page {pageIndex + 1}</span>
          <span>{page.items.length} events shown</span>
          <span>{page.total} total</span>
        </div>
        <div class="pagination-controls">
          <button class="secondary-button pagination-button" type="button" disabled={pageIndex === 0 || auditLoading} onclick={loadPreviousAuditPage}>
            <Icon icon="lucide:arrow-left" class="pagination-button-icon" aria-hidden="true" />
            <span>Prev</span>
          </button>
          <div class="pagination-pages" aria-label="Pages">
            {#each paginationItems as item, index (`${item}-${index}`)}
              {#if item === 'ellipsis'}
                <span class="pagination-ellipsis" aria-hidden="true">...</span>
              {:else}
                <button
                  class:active={item === currentPageNumber}
                  class="page-number-button"
                  type="button"
                  aria-label={`Page ${item}`}
                  aria-current={item === currentPageNumber ? 'page' : undefined}
                  disabled={auditLoading}
                  onclick={() => loadAuditPageNumber(item)}
                >
                  {item}
                </button>
              {/if}
            {/each}
          </div>
          <button class="secondary-button pagination-button" type="button" disabled={!page.nextCursor || auditLoading} onclick={loadNextAuditPage}>
            <span>Next</span>
            <Icon icon="lucide:arrow-right" class="pagination-button-icon" aria-hidden="true" />
          </button>
          <label class="page-size-control">
            <span class="sr-only">Items per page</span>
            <select class="admin-input page-size-select" bind:value={filters.pageSize} disabled={auditLoading} onchange={changePageSize}>
              <option value="10">10 Items</option>
              <option value="25">25 Items</option>
              <option value="50">50 Items</option>
              <option value="100">100 Items</option>
              <option value="200">200 Items</option>
            </select>
          </label>
        </div>
      </nav>
            </section>
          {/if}
        </div>
      </div>
    </div>
</main>

{#if detailOpen && selected}
  <div class="modal-backdrop" role="presentation">
    <div class="modal-panel detail-modal-panel" role="dialog" aria-modal="true" aria-labelledby="audit-detail-title">
      <div class="modal-header">
        <h2 id="audit-detail-title" class="text-base font-semibold">Audit event detail</h2>
        <button class="icon-button" aria-label="Close audit event detail" title="Close" onclick={() => (detailOpen = false)}>
          <Icon icon="lets-icons:close-round" class="h-5 w-5" />
        </button>
      </div>
      <div class="modal-body">
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <Detail label="ID" value={selected.id} />
          <Detail label="Timestamp" value={fmtEventTime(selected)} />
          <Detail label="Actor" value={selected.actor} />
          <Detail label="Actor source" value={selected.actorSource} />
          <Detail label="Action" value={selected.action} />
          <Detail label="Result" value={selected.result} />
          <Detail label="Method" value={selected.method} />
          <Detail label="Path" value={selected.path} />
          <Detail label="Endpoint" value={selected.endpoint} />
          <Detail label="Status code" value={String(selected.statusCode || '')} />
          <Detail label="Remote address" value={selected.remoteAddr} />
          <Detail label="Duration" value={`${selected.durationMs} ms`} />
          <Detail label="Request ID" value={selected.requestId} />
          <Detail label="Error code" value={selected.errorCode} />
          <Detail label="Message" value={selected.message} wide />
          <Detail label="User agent" value={selected.userAgent} wide />
        </div>
        {#if selected.metadata}
          <pre class="mt-4 max-h-52 overflow-auto rounded border border-[var(--border-color)] bg-slate-950 p-3 text-xs text-slate-100">{JSON.stringify(selected.metadata, null, 2)}</pre>
        {/if}
      </div>
    </div>
  </div>
{/if}

{#if loading}
  <div class="loading-overlay" role="presentation">
    <div class="loading-indicator">
      <span class="loading-spinner" aria-hidden="true"></span>
      <span role="status" aria-live="polite">Loading dashboard</span>
    </div>
  </div>
{/if}

{#if resetOpen}
  <div class="modal-backdrop" role="presentation">
    <div class="modal-panel" role="dialog" aria-modal="true" aria-labelledby="reset-audit-title">
      <div class="modal-header">
        <h2 id="reset-audit-title" class="text-base font-semibold">Reset audit log</h2>
        <button class="icon-button" aria-label="Close reset dialog" title="Close" disabled={resetting} onclick={() => (resetOpen = false)}>
          <Icon icon="lets-icons:close-round" class="h-5 w-5" />
        </button>
      </div>
      <div class="modal-body space-y-4">
        <div class="danger-callout">
          <Icon icon="lets-icons:warning" class="mt-0.5 h-5 w-5 shrink-0" />
          <div>
            <div class="font-semibold">Warning</div>
            <p class="mt-1 text-sm">Existing audit events will be removed and a new reset marker will remain visible.</p>
          </div>
        </div>
        <Field label="Confirmation">
          <input class="admin-input" bind:value={resetConfirmation} placeholder=" " autocomplete="off" aria-describedby="reset-confirmation-help" />
        </Field>
        <p id="reset-confirmation-help" class="-mt-2 text-sm text-[var(--text-secondary)]">Type RESET to enable the destructive action.</p>
        <Field label="Reason">
          <textarea class="admin-input min-h-24 resize-y" bind:value={resetReason} placeholder=" "></textarea>
        </Field>
        <div class="flex gap-2 pt-1">
          <Button color="red" disabled={resetConfirmation !== 'RESET' || resetting} aria-busy={resetting} onclick={resetAuditLog}>
            {resetting ? 'Resetting' : 'Reset'}
          </Button>
          <button class="secondary-button" type="button" disabled={resetting} onclick={() => (resetOpen = false)}>Cancel</button>
        </div>
      </div>
    </div>
  </div>
{/if}
