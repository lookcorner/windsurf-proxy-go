// Desktop builds run inside Wails, where the page hostname is not a real network
// endpoint for the embedded Go server. Use loopback there and keep browser/dev
// mode on the current hostname.
let _apiPort = 8000;
let _apiHost = resolveApiHost();

function resolveApiHost() {
  if (typeof window === 'undefined') {
    return '127.0.0.1';
  }

  const runtimeWindow = window as typeof window & {
    go?: { main?: { App?: unknown } };
  };

  if (runtimeWindow.go?.main?.App) {
    return '127.0.0.1';
  }

  return window.location.hostname || '127.0.0.1';
}

export function setApiPort(port: number) { _apiPort = port; }
export function getApiBase() { return `http://${_apiHost}:${_apiPort}`; }

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${getApiBase()}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ detail: res.statusText }));
    throw new Error(err.detail || `HTTP ${res.status}`);
  }
  return res.json();
}

// ── Stats ──
export interface Stats {
  uptime_seconds: number;
  total_requests: number;
  active_connections: number;
  instance_count: number;
  healthy_count: number;
  model_count: number;
}

export const fetchStats = () => request<Stats>('/api/stats');

// ── Instances ──
export interface Instance {
  name: string;
  type: string;
  healthy: boolean;
  active_connections: number;
  total_requests: number;
  consecutive_failures: number;
  weight: number;
  last_error: string | null;
  host: string;
  port: number;
  email: string;
}

export const fetchInstances = () =>
  request<{ instances: Instance[] }>('/api/instances').then((r) => r.instances);

export interface InstanceCreateBody {
  name: string;
  type: string;
  host?: string;
  grpc_port?: number;
  csrf_token?: string;
  api_key?: string;
  weight?: number;
  email?: string;
  password?: string;
  binary_path?: string;
  server_port?: number;
}

export const createInstance = (body: InstanceCreateBody) =>
  request<{ status: string; name: string }>('/api/instances', {
    method: 'POST',
    body: JSON.stringify(body),
  });

export const deleteInstance = (name: string) =>
  request<{ status: string }>(`/api/instances/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });

export const restartInstance = (name: string) =>
  request<{ status: string }>(`/api/instances/${encodeURIComponent(name)}/restart`, {
    method: 'POST',
  });

// ── API Keys ──
export interface ApiKey {
  id: string;
  name: string;
  key_masked: string;
  rate_limit: number;
  allowed_models: string[];
}

export const fetchKeys = () =>
  request<{ keys: ApiKey[] }>('/api/keys').then((r) => r.keys);

export interface ApiKeyCreateBody {
  name: string;
  rate_limit: number;
  allowed_models: string[];
}

export const createKey = (body: ApiKeyCreateBody) =>
  request<{ status: string; key: string; name: string }>('/api/keys', {
    method: 'POST',
    body: JSON.stringify(body),
  });

export const deleteKey = (keyId: string) =>
  request<{ status: string }>(`/api/keys/${encodeURIComponent(keyId)}`, {
    method: 'DELETE',
  });

// ── Config ──
export interface AppConfig {
  server: {
    host: string;
    port: number;
    workers: number;
    log_level: string;
  };
  balancing: {
    strategy: string;
    health_check_interval: number;
    max_retries: number;
    retry_delay: number;
  };
  logging: {
    audit: boolean;
  };
  instance_count: number;
  api_key_count: number;
}

export const fetchConfig = () => request<AppConfig>('/api/config');

export const updateConfig = (body: {
  server?: Record<string, unknown>;
  balancing?: Record<string, unknown>;
  logging?: Record<string, unknown>;
}) =>
  request<{ status: string }>('/api/config', {
    method: 'PUT',
    body: JSON.stringify(body),
  });

// ── Models ──
export const fetchModels = () =>
  request<{ models: string[] }>('/api/models').then((r) => r.models);

// ── Request History ──
export interface RequestRecord {
  id: string;
  timestamp: number;
  time_str: string;
  model: string;
  instance: string;
  stream: boolean;
  status: string;
  duration_ms: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  error: string | null;
}

export const fetchRequests = (limit = 200) =>
  request<{ requests: RequestRecord[] }>(`/api/requests?limit=${limit}`).then((r) => r.requests);
