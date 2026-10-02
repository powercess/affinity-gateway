import { Activity, ArrowUpRight, GitBranch, LayoutDashboard, Percent, Terminal } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import type { Translate } from '@/lib/i18n';
import type { GatewayConfig, Metrics, RequestRecord } from '@/lib/types';

type Props = {
  t: Translate;
  metrics: Metrics;
  requests: RequestRecord[];
  config: GatewayConfig | null;
  onViewRequests: () => void;
};

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString(undefined, { hour12: false });
}

export function Overview({ t, metrics, requests, config, onViewRequests }: Props) {
  const rate = metrics.egress_requests
    ? `${((metrics.egress_success / metrics.egress_requests) * 100).toFixed(2)}%`
    : '—';

  const stats = [
    { icon: Activity, label: t('overview.requests'), value: metrics.requests, hint: t('overview.requestsHint') },
    { icon: GitBranch, label: t('overview.sessions'), value: metrics.affinity_sessions, hint: t('overview.sessionsHint') },
    { icon: ArrowUpRight, label: t('overview.egressRequests'), value: metrics.egress_requests, hint: t('overview.egressHint') },
    { icon: Percent, label: t('overview.egressSuccess'), value: rate, hint: t('overview.successHint') },
  ];

  return (
    <section className="mx-auto w-full max-w-6xl px-6 py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">{t('overview.title')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('overview.subtitle')}</p>
        </div>
        <Button variant="outline" onClick={onViewRequests}>
          <Terminal size={15} /> {t('overview.viewRequests')}
        </Button>
      </header>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {stats.map((stat) => (
          <Card key={stat.label} className="p-5">
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>{stat.label}</span>
              <stat.icon size={16} />
            </div>
            <p className="mt-3 text-2xl font-semibold tabular-nums">
              {typeof stat.value === 'number' ? stat.value.toLocaleString() : stat.value}
            </p>
            <p className="mt-1 text-[11px] text-muted-foreground">{stat.hint}</p>
          </Card>
        ))}
      </div>

      <div className="mt-6 grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <div className="flex items-center justify-between border-b border-border p-5">
            <div>
              <h3 className="text-sm font-semibold">{t('overview.recent')}</h3>
              <p className="text-xs text-muted-foreground">{t('overview.recentHint')}</p>
            </div>
          </div>
          {requests.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-6 py-14 text-center">
              <Terminal size={26} className="text-muted-foreground" />
              <p className="text-sm font-medium">{t('overview.recentEmpty')}</p>
            </div>
          ) : (
            <ul className="divide-y divide-border">
              {requests.slice(0, 6).map((record, index) => (
                <li key={`${record.time}-${index}`} className="flex items-center gap-4 px-5 py-3 text-sm">
                  <span className="w-14 shrink-0 text-xs text-muted-foreground">
                    {record.direction === 'inbound' ? t('requests.direction.in') : t('requests.direction.out')}
                  </span>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">{record.path}</span>
                  <span className="hidden truncate text-xs text-muted-foreground sm:block">{record.model || '—'}</span>
                  <span className={record.status < 400 ? 'text-xs text-primary' : 'text-xs text-destructive'}>
                    {record.status}
                  </span>
                  <span className="text-xs text-muted-foreground tabular-nums">{formatTime(record.time)}</span>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card className="p-5">
          <div className="flex items-center gap-2">
            <LayoutDashboard size={16} className="text-muted-foreground" />
            <h3 className="text-sm font-semibold">{t('overview.routes')}</h3>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">{t('overview.routesHint')}</p>
          <div className="mt-4 flex flex-col gap-3">
            <div className="flex items-center justify-between rounded-lg bg-muted px-4 py-3">
              <span className="text-sm text-muted-foreground">{t('overview.inboundCount')}</span>
              <span className="text-lg font-semibold tabular-nums">{config?.inbound.length ?? 0}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted px-4 py-3">
              <span className="text-sm text-muted-foreground">{t('overview.egressCount')}</span>
              <span className="text-lg font-semibold tabular-nums">{config?.egress.length ?? 0}</span>
            </div>
          </div>
        </Card>
      </div>
    </section>
  );
}
