import type { GatewayConfig, Metrics, Plugin, RequestDetail, RequestRecord, Route } from './types';

const TOKEN_KEY = 'affinity.token';

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setToken(token: string): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* ignore storage failures */
  }
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

function normalizeRoute(route: Route): Route {
  return { ...route, plugins: route.plugins ?? [] };
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const token = getToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);

  const response = await fetch(path, { ...init, headers });
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload?.error) message = payload.error;
    } catch {
      /* keep the status message */
    }
    throw new ApiError(response.status, message);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export const api = {
  config: async (): Promise<GatewayConfig> => {
    const config = await request<GatewayConfig>('/api/v1/config');
    const list = (routes: Route[] | null | undefined) =>
      (Array.isArray(routes) ? routes : []).map(normalizeRoute);
    return { ...config, inbound: list(config.inbound), egress: list(config.egress) };
  },
  metrics: () => request<Metrics>('/api/v1/metrics'),
  plugins: () => request<Plugin[]>('/api/v1/plugins'),
  requests: () => request<RequestRecord[]>('/api/v1/requests'),
  requestDetail: (id: number) => request<RequestDetail>(`/api/v1/requests/${id}`),

  createRoute: (direction: 'inbound' | 'egress', route: unknown) =>
    request(`/api/v1/${direction}`, { method: 'POST', body: JSON.stringify(route) }),
  updateRoute: (direction: 'inbound' | 'egress', id: string, route: unknown) =>
    request(`/api/v1/${direction}/${id}`, { method: 'PUT', body: JSON.stringify(route) }),
  deleteRoute: (direction: 'inbound' | 'egress', id: string) =>
    request(`/api/v1/${direction}/${id}`, { method: 'DELETE' }),

  savePlugin: (plugin: Plugin) =>
    request<Plugin>(`/api/v1/plugins/${plugin.id}`, { method: 'PUT', body: JSON.stringify(plugin) }),
  deletePlugin: (id: string) => request(`/api/v1/plugins/${id}`, { method: 'DELETE' }),
};
