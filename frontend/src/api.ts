export type AdminMe = {
  mode: string;
  authDisabled: boolean;
  user: string;
  email: string;
  groups: string[];
  version: string;
  commit: string;
  shortCommit: string;
  commitURL: string;
  auditTimestampFormat: string;
};

export type Summary = {
  total: number;
  successful: number;
  failed: number;
  exchangeSuccesses: number;
  exchangeFailures: number;
  verifySuccesses: number;
  verifyFailures: number;
  count4xx: number;
  count5xx: number;
};

export type MetricPoint = {
  timestamp: string;
  count: number;
};

export type MetricsResponse = {
  from: string;
  to: string;
  bucket: string;
  points: MetricPoint[];
  summary: Summary;
};

export type AuditEvent = {
  id: string;
  timestamp: string;
  timestampDisplay?: string;
  actor: string;
  actorSource: string;
  action: string;
  method: string;
  path: string;
  endpoint: string;
  statusCode: number;
  result: string;
  remoteAddr: string;
  userAgent: string;
  requestId: string;
  durationMs: number;
  errorCode: string;
  message: string;
  metadata?: Record<string, unknown>;
};

export type AuditPage = {
  items: AuditEvent[];
  total: number;
  nextCursor: string | null;
  hasNext: boolean;
};

export type AuditOptions = {
  actions: string[];
  endpoints: string[];
  results: string[];
};

export type AuditFilters = {
  range: string;
  from: string;
  to: string;
  pageSize: string;
  actor: string;
  action: string;
  endpoint: string;
  path: string;
  method: string;
  result: string;
  statusCode: string;
  requestId: string;
};

const config = window.TACG_ADMIN ?? {
  apiBasePath: '/_admin/api/v1',
  dashboardBasePath: '/admin'
};

const transientAuthRetryDelays = [500, 1500];

export class ApiError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string) {
    super(body || `HTTP ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

export function apiBasePath(): string {
  return config.apiBasePath.replace(/\/$/, '');
}

export async function apiGet<T>(path: string, params?: URLSearchParams): Promise<T> {
  const suffix = params && params.toString() ? `${path}?${params.toString()}` : path;
  const response = await fetch(`${apiBasePath()}${suffix}`, {
    headers: { Accept: 'application/json' }
  });
  if (!response.ok) {
    const body = await response.text();
    throw new ApiError(response.status, body);
  }
  return response.json() as Promise<T>;
}

export async function apiGetWithAuthRecovery<T>(path: string, params?: URLSearchParams): Promise<T> {
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await apiGet<T>(path, params);
    } catch (err) {
      if (!(err instanceof ApiError) || !isTransientAuthStatus(err.status) || attempt >= transientAuthRetryDelays.length) {
        throw err;
      }
      await sleep(transientAuthRetryDelays[attempt]);
    }
  }
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(`${apiBasePath()}${path}`, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(body)
  });
  if (!response.ok) {
    const responseBody = await response.text();
    throw new ApiError(response.status, responseBody);
  }
  return response.json() as Promise<T>;
}

export function isTransientAuthStatus(status: number): boolean {
  return status === 401 || status === 403;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function rangeToParams(range: string): URLSearchParams {
  const now = new Date();
  const from = new Date(now.getTime() - rangeMillis(range));
  const params = new URLSearchParams();
  params.set('from', from.toISOString());
  params.set('to', now.toISOString());
  return params;
}

export function rangeMillis(range: string): number {
  if (range === '1h') return 60 * 60 * 1000;
  if (range === '6h') return 6 * 60 * 60 * 1000;
  if (range === '7d') return 7 * 24 * 60 * 60 * 1000;
  if (range === '30d') return 30 * 24 * 60 * 60 * 1000;
  return 24 * 60 * 60 * 1000;
}

export function auditParams(filters: AuditFilters, limit: number, cursor: string): URLSearchParams {
  const params = new URLSearchParams();
  if (filters.from) {
    params.set('from', new Date(filters.from).toISOString());
  }
  if (filters.to) {
    params.set('to', new Date(filters.to).toISOString());
  }
  if (!filters.from && !filters.to) {
    const rangeParams = rangeToParams(filters.range);
    params.set('from', rangeParams.get('from') ?? '');
    params.set('to', rangeParams.get('to') ?? '');
  }
  params.set('limit', String(limit));
  if (cursor) params.set('cursor', cursor);
  const entries: Array<[string, string]> = [
    ['actor', filters.actor],
    ['action', filters.action],
    ['endpoint', filters.endpoint],
    ['path', filters.path],
    ['method', filters.method],
    ['result', filters.result],
    ['status_code', filters.statusCode],
    ['request_id', filters.requestId]
  ];
  for (const [key, value] of entries) {
    const trimmed = value.trim();
    if (trimmed) params.set(key, trimmed);
  }
  return params;
}
