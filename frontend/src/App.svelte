<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from '@iconify/svelte';
  import { Alert, Badge, Button, Modal } from 'flowbite-svelte';
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
      ctx.fillStyle = '#475569';
      ctx.font = `600 12px ${fontFamily}`;
      ctx.fillText('Total', centerX, centerY - 9);
      ctx.fillStyle = '#0f172a';
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

  type ActiveView = 'dashboard' | 'audit';

  let me: AdminMe | null = null;
  let metrics: MetricsResponse | null = null;
  let auditOptions: AuditOptions = { actions: [], endpoints: [], results: [] };
  let page: AuditPage = { items: [], total: 0, nextCursor: null };
  let activeView: ActiveView = 'dashboard';
  let selected: AuditEvent | null = null;
  let detailOpen = false;
  let resetOpen = false;
  let resetConfirmation = '';
  let resetReason = '';
  let resetting = false;
  let loading = true;
  let error = '';
  let recoverableAuthError = false;
  let offset = 0;
  let filters: AuditFilters = {
    range: '24h',
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
        borderColor: '#2563eb',
        backgroundColor: 'rgba(37, 99, 235, 0.12)',
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
      x: { ticks: { maxRotation: 0, autoSkip: true, maxTicksLimit: 8 } },
      y: { beginAtZero: true, ticks: { precision: 0 } }
    }
  };
  $: doughnutOptions = {
    responsive: true,
    maintainAspectRatio: false,
    cutout: '62%',
    plugins: {
      legend: {
        position: 'bottom' as const,
        labels: { boxWidth: 10, usePointStyle: true }
      }
    }
  };

  onMount(() => {
    syncViewFromHash();
    window.addEventListener('hashchange', syncViewFromHash);
    reloadAll();
    return () => window.removeEventListener('hashchange', syncViewFromHash);
  });

  async function reloadAll() {
    loading = true;
    error = '';
    recoverableAuthError = false;
    try {
      me = await apiGetWithAuthRecovery<AdminMe>('/me');
      auditOptions = await apiGetWithAuthRecovery<AuditOptions>('/audit-options');
      await loadMetrics(true);
      await loadAudit(0, true);
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
    const params = rangeToParams(filters.range);
    if (filters.endpoint) params.set('endpoint', filters.endpoint);
    if (filters.method) params.set('method', filters.method);
    if (filters.result) params.set('result', filters.result);
    metrics = withAuthRecovery
      ? await apiGetWithAuthRecovery<MetricsResponse>('/request-metrics', params)
      : await apiGet<MetricsResponse>('/request-metrics', params);
  }

  async function loadAudit(nextOffset: number, withAuthRecovery = false) {
    offset = Math.max(0, nextOffset);
    const params = auditParams(filters, pageSize, offset);
    page = withAuthRecovery ? await apiGetWithAuthRecovery<AuditPage>('/audit-events', params) : await apiGet<AuditPage>('/audit-events', params);
  }

  async function applyFilters() {
    error = '';
    recoverableAuthError = false;
    try {
      await loadMetrics();
      await loadAudit(0);
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
    window.location.hash = view === 'audit' ? 'audit-log' : 'dashboard';
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

<main class="min-h-screen bg-slate-100 text-slate-950">
  <div class="min-h-screen lg:flex">
    <aside class="flex flex-col border-b border-slate-200 bg-white shadow-sm lg:fixed lg:inset-y-0 lg:w-64 lg:border-b-0 lg:border-r">
      <div class="px-5 py-6">
        <div class="flex w-fit items-start gap-1.5">
          <img src="./turnstile-appcheck-gateway-logo-str.svg" alt="turnstile-appcheck-gateway" class="h-10 w-auto max-w-32 md:h-11" />
          {#if me}
            <span class="mt-1 whitespace-nowrap text-sm font-medium text-slate-700">{me.version}</span>
          {/if}
        </div>
      </div>

      <nav class="flex gap-2 overflow-x-auto border-t border-slate-100 px-3 py-3 lg:flex-col lg:overflow-visible lg:border-t-0">
        <button
          class={`inline-flex min-w-fit items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition ${activeView === 'dashboard' ? 'bg-slate-100 text-blue-700' : 'text-slate-600 hover:bg-slate-50 hover:text-slate-950'}`}
          aria-current={activeView === 'dashboard' ? 'page' : undefined}
          onclick={() => setView('dashboard')}
        >
          <Icon icon="lets-icons:chart" class="h-5 w-5" />
          <span>Dashboard</span>
        </button>
        <button
          class={`inline-flex min-w-fit items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition ${activeView === 'audit' ? 'bg-slate-100 text-blue-700' : 'text-slate-600 hover:bg-slate-50 hover:text-slate-950'}`}
          aria-current={activeView === 'audit' ? 'page' : undefined}
          onclick={() => setView('audit')}
        >
          <Icon icon="lets-icons:order" class="h-5 w-5" />
          <span>Audit Log</span>
        </button>
      </nav>

      <div class="mt-auto hidden border-t border-slate-200 px-5 py-4 text-sm text-slate-600 lg:block">
        {#if me}
          {#if me.commitURL}
            <a class="inline-flex max-w-full items-center gap-1.5 truncate text-slate-700 hover:text-blue-700 hover:underline" href={me.commitURL} target="_blank" rel="noreferrer">
              <Icon icon="mdi:github" class="h-4 w-4 shrink-0" />
              <span class="truncate">{me.shortCommit}</span>
            </a>
          {:else}
            <span class="truncate">Commit {me.shortCommit}</span>
          {/if}
        {/if}
      </div>
    </aside>

    <div class="flex min-h-screen flex-1 flex-col lg:pl-64">
      <header class="sticky top-0 z-20 flex min-h-16 items-center justify-end border-b border-slate-200 bg-slate-100/95 px-4 backdrop-blur sm:px-6 lg:px-8">
        <div class="flex flex-col items-end gap-1 text-sm text-slate-600">
          {#if me}
            <div>
              <span class="font-medium text-slate-900">{me.user}</span>
              <span class="mx-2">/</span>
              <span>{me.mode}</span>
            </div>
          {/if}
        </div>
      </header>

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
              <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
                <h2 class="text-sm font-semibold text-slate-700">Public gateway request result</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(publicApiValues)}
                    <Doughnut data={publicApiData} options={doughnutOptions} />
                  {:else}
                    <div class="mx-auto flex aspect-square w-full max-w-52 items-center justify-center rounded-full border-[18px] border-slate-200 text-sm font-medium text-slate-500">No data</div>
                  {/if}
                </div>
              </div>
              <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
                <h2 class="text-sm font-semibold text-slate-700">Exchange request result</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(exchangeValues)}
                    <Doughnut data={exchangeData} options={doughnutOptions} />
                  {:else}
                    <div class="mx-auto flex aspect-square w-full max-w-52 items-center justify-center rounded-full border-[18px] border-slate-200 text-sm font-medium text-slate-500">No data</div>
                  {/if}
                </div>
              </div>
              <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
                <h2 class="text-sm font-semibold text-slate-700">Verify request result</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(verifyValues)}
                    <Doughnut data={verifyData} options={doughnutOptions} />
                  {:else}
                    <div class="mx-auto flex aspect-square w-full max-w-52 items-center justify-center rounded-full border-[18px] border-slate-200 text-sm font-medium text-slate-500">No data</div>
                  {/if}
                </div>
              </div>
              <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
                <h2 class="text-sm font-semibold text-slate-700">Error status class</h2>
                <div class="mt-3 h-56">
                  {#if hasCounts(statusClassValues)}
                    <Doughnut data={statusClassData} options={doughnutOptions} />
                  {:else}
                    <div class="mx-auto flex aspect-square w-full max-w-52 items-center justify-center rounded-full border-[18px] border-slate-200 text-sm font-medium text-slate-500">No data</div>
                  {/if}
                </div>
              </div>
            </section>

            <section class="w-full rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
              <div class="mb-4 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div>
                  <h2 class="text-lg font-semibold">Request trend</h2>
                  <p class="text-sm text-slate-500">Request volume over the selected time range</p>
                </div>
                <div class="flex flex-wrap gap-2">
                  {#each ranges as range}
                    <Button size="sm" color={filters.range === range.value ? 'blue' : 'alternative'} onclick={async () => { filters.range = range.value; await applyFilters(); }}>
                      {range.label}
                    </Button>
                  {/each}
                </div>
              </div>
              <div class="h-72">
                {#if loading}
                  <div class="flex h-full items-center justify-center text-sm text-slate-500">Loading chart</div>
                {:else}
                  <Line data={chartData} options={chartOptions} />
                {/if}
              </div>
            </section>
          {:else}
            <section class="w-full overflow-hidden rounded-lg border border-slate-300 bg-white shadow-sm">
              <div class="flex items-center justify-end border-b border-slate-200 px-5 py-4">
                <div class="flex flex-wrap justify-end gap-2">
                  <Button color="alternative" onclick={reloadAll}>Refresh</Button>
                  <Button color="red" onclick={openResetModal}>Reset</Button>
                </div>
              </div>

      <div class="border-b border-slate-200 p-5">
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Field label="Actor"><input class="admin-input" bind:value={filters.actor} autocomplete="off" /></Field>
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
          <Field label="From"><input class="admin-input" type="datetime-local" bind:value={filters.from} /></Field>
          <Field label="To"><input class="admin-input" type="datetime-local" bind:value={filters.to} /></Field>
          <Field label="Page size"><input class="admin-input" type="number" min="1" max="200" bind:value={filters.pageSize} /></Field>
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
          <Field label="Request ID"><input class="admin-input min-w-72" bind:value={filters.requestId} autocomplete="off" /></Field>
          <Field label="Path"><input class="admin-input min-w-72" bind:value={filters.path} placeholder="/api/v1" autocomplete="off" /></Field>
        </div>
        <div class="mt-5 flex flex-wrap gap-2 border-t border-slate-100 pt-4">
          <Button color="blue" onclick={applyFilters}>Apply</Button>
          <Button color="alternative" onclick={async () => { resetFilters(); await applyFilters(); }}>Clear filters</Button>
        </div>
      </div>

      <div class="px-5 py-3 text-sm text-slate-500">{page.total} events match the current filters</div>

      <div class="hidden px-5 md:block">
        <div class="overflow-x-auto pb-3">
          <table class="w-full min-w-[1040px] border-collapse border-t border-slate-300 text-left text-sm">
            <thead class="bg-slate-50 text-xs text-slate-600">
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
            <tbody class="divide-y divide-slate-200 text-slate-700">
              {#each page.items as item}
                <tr class="hover:bg-slate-50">
                  <td class="whitespace-nowrap px-4 py-4 text-slate-900">{fmtEventTime(item)}</td>
                  <td class="px-4 py-4 text-slate-900">{item.actor}</td>
                  <td class="px-4 py-4">
                    <Badge color="blue">{item.action}</Badge>
                  </td>
                  <td class="max-w-56 truncate px-4 py-4">{eventTarget(item)}</td>
                  <td class="px-4 py-3"><Badge color={resultColor(item.result)}>{item.result}</Badge></td>
                  <td class="max-w-48 truncate px-4 py-4">{item.remoteAddr}</td>
                  <td class="max-w-72 px-4 py-4 text-slate-900">{eventMessage(item)}</td>
                  <td class="px-4 py-4 text-right">
                    <Button size="xs" color="alternative" onclick={() => showDetail(item)}>
                      <Icon icon="lets-icons:view" class="h-4 w-4" />
                      <span class="sr-only">Show details</span>
                    </Button>
                  </td>
                </tr>
              {:else}
                <tr>
                  <td colspan={8} class="px-4 py-8 text-center text-slate-500">No audit events</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>

      <div class="space-y-4 px-4 pb-3 md:hidden">
        {#each page.items as item}
          <article class="rounded-lg border border-slate-300 bg-white p-4 shadow-sm">
            <div class="text-sm font-medium text-slate-800">{fmtEventTime(item)}</div>
            <div class="mt-2 text-sm font-semibold text-slate-950">{item.actor}</div>
            <div class="mt-4 flex flex-wrap gap-2">
              <Badge color="blue">{item.action}</Badge>
              <Badge color={resultColor(item.result)}>{item.result}</Badge>
            </div>
            <p class="mt-4 text-sm text-slate-950">{eventMessage(item)}</p>
            <div class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div>
                <div class="text-xs font-medium text-slate-500">Target</div>
                <div class="mt-1 break-words text-slate-950">{eventTarget(item)}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-slate-500">Status</div>
                <div class="mt-1 text-slate-950">{item.statusCode || '-'}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-slate-500">Remote</div>
                <div class="mt-1 break-words text-slate-950">{item.remoteAddr || '-'}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-slate-500">Duration</div>
                <div class="mt-1 text-slate-950">{item.durationMs} ms</div>
              </div>
            </div>
            <Button class="mt-4" color="alternative" onclick={() => showDetail(item)}>Show details</Button>
          </article>
        {:else}
          <div class="rounded-lg border border-slate-200 bg-white px-4 py-8 text-center text-sm text-slate-500">No audit events</div>
        {/each}
      </div>

      <div class="flex items-center justify-between px-5 pb-5 pt-3 text-sm">
        <span>Showing {page.items.length ? offset + 1 : 0}-{offset + page.items.length} of {page.total}</span>
        <div class="flex gap-2">
          <Button size="sm" color="alternative" disabled={offset === 0} onclick={() => loadAudit(Math.max(0, offset - pageSize))}>Previous</Button>
          <Button size="sm" color="alternative" disabled={page.nextCursor === null} onclick={() => loadAudit(page.nextCursor ?? offset)}>Next</Button>
        </div>
      </div>
            </section>
          {/if}
        </div>
      </div>
    </div>
  </div>
</main>

<Modal bind:open={detailOpen} title="Audit event detail" size="lg">
  {#if selected}
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
      <pre class="mt-4 max-h-52 overflow-auto rounded border border-slate-200 bg-slate-950 p-3 text-xs text-slate-100">{JSON.stringify(selected.metadata, null, 2)}</pre>
    {/if}
  {/if}
</Modal>

{#if resetOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4">
    <section class="w-full max-w-xl overflow-hidden rounded-lg border border-slate-300 bg-white shadow-xl">
      <div class="flex items-center justify-between border-b border-slate-200 px-5 py-4">
        <h2 class="text-base font-semibold text-slate-950">Reset audit log</h2>
        <button class="rounded p-1 text-slate-500 hover:bg-slate-100" aria-label="Close reset dialog" disabled={resetting} onclick={() => (resetOpen = false)}>
          <Icon icon="lets-icons:close-round" class="h-5 w-5" />
        </button>
      </div>
      <div class="space-y-4 px-5 py-5">
        <div class="flex gap-3 rounded-lg border border-red-300 bg-red-50 p-4 text-red-700">
          <Icon icon="lets-icons:warning" class="mt-0.5 h-5 w-5 shrink-0" />
          <div>
            <div class="font-semibold">Warning</div>
            <p class="mt-1 text-sm">Existing audit events will be removed and a new reset marker will remain visible.</p>
          </div>
        </div>
        <Field label="Confirmation">
          <input class="admin-input" bind:value={resetConfirmation} placeholder="RESET" autocomplete="off" />
        </Field>
        <Field label="Reason">
          <textarea class="admin-input min-h-24 resize-y" bind:value={resetReason}></textarea>
        </Field>
        <div class="flex gap-2 pt-1">
          <Button color="red" disabled={resetConfirmation !== 'RESET' || resetting} onclick={resetAuditLog}>
            {resetting ? 'Resetting' : 'Reset'}
          </Button>
          <Button color="alternative" disabled={resetting} onclick={() => (resetOpen = false)}>Cancel</Button>
        </div>
      </div>
    </section>
  </div>
{/if}
