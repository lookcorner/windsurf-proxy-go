// Desktop builds run inside Wails, where the page hostname is not a real network
// endpoint for the embedded Go server. Use loopback there and keep browser/dev
// mode on the current hostname.
let _apiPort = 8000;
const _apiHost = resolveApiHost();

function resolveApiHost() {
  if (typeof window === "undefined") {
    return "127.0.0.1";
  }

  const runtimeWindow = window as typeof window & {
    go?: { main?: { App?: unknown } };
  };

  if (runtimeWindow.go?.main?.App) {
    return "127.0.0.1";
  }

  return window.location.hostname || "127.0.0.1";
}

export function setApiPort(port: number) {
  _apiPort = port;
}
export function getApiBase() {
  return `http://${_apiHost}:${_apiPort}`;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${getApiBase()}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const raw = await res.text();
  if (!res.ok) {
    let detail = raw || `HTTP ${res.status}`;
    try {
      const err = JSON.parse(raw);
      detail = err.detail || err.error || detail;
    } catch {
      // Keep the raw response body when the error payload is not JSON.
    }
    throw new Error(detail);
  }
  return raw ? JSON.parse(raw) as T : ({} as T);
}

// ── Stats ──
export interface Stats {
  uptime_seconds: number;
  total_requests: number;
  active_connections: number;
  instance_count: number;
  account_count: number;
  healthy_count: number;
  model_count: number;
}

export const fetchStats = () => request<Stats>("/api/stats");

// ── Accounts ──
export interface Account {
  id: string;
  name: string;
  provider: string;
  status: string;
  email: string;
  auth_source: string;
  has_password: boolean;
  has_refresh_token: boolean;
  has_api_key: boolean;
  key_masked: string;
  api_server: string;
  proxy: string;
  available_models: string[];
  synced_available_models: string[];
  blocked_models: string[];
  attached_instances: number;
  healthy: boolean;
  active_requests: number;
  total_requests: number;
  consecutive_failures: number;
  last_error: string;
  last_used_unix: number;
  usage_status: string;
  quota_exhausted: boolean;
  quota_low: boolean;
  lowest_quota_percent: number;
  plan_name: string;
  used_prompt_credits: number;
  used_flow_credits: number;
  available_prompt_credits: number;
  available_flow_credits: number;
  daily_quota_remaining_percent: number;
  weekly_quota_remaining_percent: number;
  daily_quota_reset_unix: number;
  weekly_quota_reset_unix: number;
  hide_daily_quota: boolean;
  hide_weekly_quota: boolean;
  last_usage_check_unix: number;
  last_model_sync_unix: number;
  usage_error: string;
}

export interface AccountCreateBody {
  name: string;
  email?: string;
  password?: string;
  api_key?: string;
  api_server?: string;
  proxy?: string;
  firebase_refresh_token?: string;
}

export const fetchAccounts = () =>
  request<{ accounts: Account[] }>("/api/accounts").then((r) => r.accounts);

export const createAccount = (body: AccountCreateBody) =>
  request<{ status: string; id: string; name: string }>("/api/accounts", {
    method: "POST",
    body: JSON.stringify(body),
  });

export const deleteAccount = (id: string) =>
  request<{ status: string }>(`/api/accounts/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });

export const refreshAccount = (id: string) =>
  request<{ status: string; result: { id: string; name: string; auth_source: string; has_api_key: boolean; usage_status: string; quota_exhausted: boolean; error?: string } }>(
    `/api/accounts/${encodeURIComponent(id)}/refresh`,
    {
      method: "POST",
    },
  );

export const refreshAllAccounts = () =>
  request<{
    status: string;
    total: number;
    succeeded: number;
    failed: number;
    exhausted: number;
    results: Array<{
      id: string;
      name: string;
      auth_source: string;
      has_api_key: boolean;
      usage_status: string;
      quota_exhausted: boolean;
      error?: string;
    }>;
  }>("/api/accounts/refresh-all", {
    method: "POST",
  });

// ── Instances ──
export interface Instance {
  name: string;
  type: string;
  auth_source: string;
  account_id: string;
  account_name: string;
  healthy: boolean;
  active_connections: number;
  total_requests: number;
  consecutive_failures: number;
  weight: number;
  last_error: string | null;
  host: string;
  port: number;
  proxy: string;
  email: string;
}

export const fetchInstances = () =>
  request<{ instances: Instance[] }>("/api/instances").then((r) => r.instances);

export interface InstanceCreateBody {
  name: string;
  type: string;
  host?: string;
  grpc_port?: number;
  csrf_token?: string;
  api_key?: string;
  proxy?: string;
  weight?: number;
  account_id?: string;
  binary_path?: string;
  server_port?: number;
}

export const createInstance = (body: InstanceCreateBody) =>
  request<{ status: string; name: string }>("/api/instances", {
    method: "POST",
    body: JSON.stringify(body),
  });

export const deleteInstance = (name: string) =>
  request<{ status: string }>(`/api/instances/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });

export const restartInstance = (name: string) =>
  request<{ status: string }>(
    `/api/instances/${encodeURIComponent(name)}/restart`,
    {
      method: "POST",
    },
  );

// ── API Keys ──
export interface ApiKey {
  id: string;
  name: string;
  key_masked: string;
  rate_limit: number;
  allowed_models: string[];
}

export const fetchKeys = () =>
  request<{ keys: ApiKey[] }>("/api/keys").then((r) => r.keys);

export interface ApiKeyCreateBody {
  name: string;
  rate_limit: number;
  allowed_models: string[];
}

export const createKey = (body: ApiKeyCreateBody) =>
  request<{ status: string; key: string; name: string }>("/api/keys", {
    method: "POST",
    body: JSON.stringify(body),
  });

export const deleteKey = (keyId: string) =>
  request<{ status: string }>(`/api/keys/${encodeURIComponent(keyId)}`, {
    method: "DELETE",
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
  account_count: number;
  api_key_count: number;
}

export const fetchConfig = () => request<AppConfig>("/api/config");

export const updateConfig = (body: {
  server?: Record<string, unknown>;
  balancing?: Record<string, unknown>;
  logging?: Record<string, unknown>;
}) =>
  request<{ status: string }>("/api/config", {
    method: "PUT",
    body: JSON.stringify(body),
  });

// ── Models ──
export const fetchModels = () =>
  request<{ models: string[] }>("/api/models").then((r) => r.models);

// ── Request History ──
export interface RequestRecord {
  id: string;
  timestamp: number;
  time_str: string;
  model: string;
  instance: string;
  account: string;
  stream: boolean;
  status: string;
  duration_ms: number;
  turns: number;
  prompt_chars: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  error: string | null;
}

export const fetchRequests = (limit = 200) =>
  request<{ requests: RequestRecord[] }>(`/api/requests?limit=${limit}`).then(
    (r) => r.requests,
  );
