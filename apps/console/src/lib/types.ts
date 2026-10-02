export type Direction = 'inbound' | 'egress';
export type RouteState = 'active' | 'inactive';

export type Route = {
  id: string;
  name: string;
  path: string;
  target: string;
  state: RouteState;
  plugins: string[];
};

export type PluginDirection = 'inbound' | 'egress' | 'both';

export type Plugin = {
  id: string;
  direction: PluginDirection;
  description: string;
  source: string;
};

export type Metrics = {
  requests: number;
  affinity_sessions: number;
  egress_requests: number;
  egress_success: number;
};

export type RequestRecord = {
  id: number;
  time: string;
  direction: Direction;
  route: string;
  method: string;
  path: string;
  model: string;
  status: number;
  session: string;
  session_source: string;
  latency_ms: number;
  target: string;
  error?: string;
};

export type RequestDetail = RequestRecord & {
  request_headers?: Record<string, string[]>;
};

export type GatewayConfig = {
  version: number;
  listen: string;
  inbound: Route[];
  egress: Route[];
  plugins: Plugin[];
  updated_at: string;
};

export const emptyMetrics: Metrics = {
  requests: 0,
  affinity_sessions: 0,
  egress_requests: 0,
  egress_success: 0,
};

export function emptyRoute(direction: Direction): Route {
  return {
    id: '',
    name: '',
    path: direction === 'inbound' ? '' : '',
    target: direction === 'inbound' ? 'http://127.0.0.1:9000' : 'https://api.example.com',
    state: 'active',
    plugins: direction === 'inbound' ? [] : ['opencode.session'],
  };
}

export function emptyPlugin(): Plugin {
  return {
    id: '',
    direction: 'both',
    description: '',
    source: 'function handle(ctx)\n  return "continue"\nend\n',
  };
}
