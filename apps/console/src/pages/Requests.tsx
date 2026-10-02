import { useEffect, useMemo, useState } from 'react';
import { Search, Terminal } from 'lucide-react';
import { Card } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { api } from '@/lib/api';
import type { Translate } from '@/lib/i18n';
import type { RequestDetail, RequestRecord } from '@/lib/types';

type Props = {
  t: Translate;
  requests: RequestRecord[];
  query: string;
  onQueryChange: (value: string) => void;
};

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, { hour12: false });
}

export function Requests({ t, requests, query, onQueryChange }: Props) {
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [detail, setDetail] = useState<RequestDetail | null>(null);
  const [detailError, setDetailError] = useState('');

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return requests;
    return requests.filter((record) =>
      [record.path, record.route, record.model, record.session, String(record.status)]
        .join(' ')
        .toLowerCase()
        .includes(needle),
    );
  }, [requests, query]);

  useEffect(() => {
    if (selectedId == null) {
      setDetail(null);
      setDetailError('');
      return;
    }
    let active = true;
    setDetail(null);
    setDetailError('');
    api
      .requestDetail(selectedId)
      .then((record) => {
        if (active) setDetail(record);
      })
      .catch((error: unknown) => {
        if (active) setDetailError(error instanceof Error ? error.message : String(error));
      });
    return () => {
      active = false;
    };
  }, [selectedId]);

  const headers = useMemo(
    () => Object.entries(detail?.request_headers ?? {}).sort(([a], [b]) => a.localeCompare(b)),
    [detail],
  );

  return (
    <section className="mx-auto w-full max-w-6xl px-6 py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">{t('requests.title')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('requests.subtitle')}</p>
        </div>
        <label className="relative w-full max-w-xs">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder={t('requests.searchHint')}
            className="pl-9"
          />
        </label>
      </header>

      {filtered.length === 0 ? (
        <Card className="flex flex-col items-center gap-2 px-6 py-16 text-center">
          <Terminal size={28} className="text-muted-foreground" />
          <h3 className="text-sm font-medium">{t('requests.empty')}</h3>
          <p className="text-xs text-muted-foreground">{t('requests.emptyHint')}</p>
        </Card>
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-left text-sm">
              <thead>
                <tr className="border-b border-border text-[11px] uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-3 font-medium">{t('requests.col.time')}</th>
                  <th className="px-5 py-3 font-medium">{t('requests.col.direction')}</th>
                  <th className="px-5 py-3 font-medium">{t('requests.col.route')}</th>
                  <th className="px-5 py-3 font-medium">{t('requests.col.model')}</th>
                  <th className="px-5 py-3 font-medium">{t('requests.col.session')}</th>
                  <th className="px-5 py-3 font-medium">{t('requests.col.status')}</th>
                  <th className="px-5 py-3 font-medium">{t('requests.col.latency')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filtered.map((record) => (
                  <tr
                    key={record.id}
                    onClick={() => setSelectedId(record.id)}
                    className="cursor-pointer transition-colors hover:bg-muted"
                  >
                    <td className="whitespace-nowrap px-5 py-3 font-mono text-xs">{formatTime(record.time)}</td>
                    <td className="px-5 py-3 text-xs">
                      {record.direction === 'inbound' ? t('requests.direction.in') : t('requests.direction.out')}
                    </td>
                    <td className="px-5 py-3 text-xs">{record.route || '—'}</td>
                    <td className="px-5 py-3 text-xs">{record.model || '—'}</td>
                    <td className="max-w-[180px] truncate px-5 py-3 font-mono text-xs text-muted-foreground">
                      {record.session || '—'}
                    </td>
                    <td className={`px-5 py-3 text-xs ${record.status < 400 ? 'text-primary' : 'text-destructive'}`}>
                      {record.status}
                    </td>
                    <td className="px-5 py-3 text-xs tabular-nums text-muted-foreground">
                      {record.latency_ms} ms
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <Dialog open={selectedId != null} onOpenChange={(open) => !open && setSelectedId(null)}>
        <DialogContent className="w-[min(720px,calc(100vw-2rem))]">
          <DialogHeader>
            <DialogTitle>{t('requests.detailTitle')}</DialogTitle>
          </DialogHeader>

          {detailError ? <p className="text-sm text-destructive">{detailError}</p> : null}
          {detail ? (
            <div className="flex flex-col gap-5">
              <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-3">
                <Detail label={t('requests.col.time')} value={formatTime(detail.time)} />
                <Detail
                  label={t('requests.col.direction')}
                  value={detail.direction === 'inbound' ? t('requests.direction.in') : t('requests.direction.out')}
                />
                <Detail label={t('requests.col.route')} value={detail.route || '—'} />
                <Detail label={t('requests.detail.method')} value={detail.method} />
                <Detail label={t('requests.detail.path')} value={detail.path} mono />
                <Detail label={t('requests.col.model')} value={detail.model || '—'} />
                <Detail label={t('requests.col.session')} value={detail.session || '—'} mono />
                <Detail label={t('requests.detail.sessionSource')} value={detail.session_source || '—'} />
                <Detail label={t('requests.detail.target')} value={detail.target || '—'} mono />
                <Detail label={t('requests.col.status')} value={`${detail.status} · ${detail.latency_ms} ms`} />
                {detail.error ? <Detail label={t('requests.detail.error')} value={detail.error} /> : null}
              </dl>

              <div>
                <p className="mb-2 text-xs font-medium text-muted-foreground">
                  {t('requests.detail.headers')}
                  <span className="ml-2 font-normal">({t('requests.detail.redacted')})</span>
                </p>
                {headers.length ? (
                  <div className="overflow-hidden rounded-lg border border-border">
                    <table className="w-full text-left text-xs">
                      <tbody className="divide-y divide-border">
                        {headers.map(([name, values]) => (
                          <tr key={name}>
                            <td className="w-1/3 break-all bg-muted px-3 py-2 font-mono text-muted-foreground">{name}</td>
                            <td className="break-all px-3 py-2 font-mono">{values.join(', ')}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">—</p>
                )}
              </div>
            </div>
          ) : !detailError ? (
            <p className="py-6 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : null}
        </DialogContent>
      </Dialog>
    </section>
  );
}

function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className={mono ? 'break-all font-mono text-xs' : 'break-all text-sm'}>{value}</dd>
    </div>
  );
}
