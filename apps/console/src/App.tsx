import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowDownLeft,
  ArrowUpRight,
  Code2,
  LayoutDashboard,
  Menu,
  Monitor,
  Moon,
  Network,
  RefreshCw,
  Search,
  Settings2,
  Sun,
  Terminal,
  X,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useToast } from '@/components/ui/toast';
import { Overview } from '@/pages/Overview';
import { RoutesPage } from '@/pages/RoutesPage';
import { Requests } from '@/pages/Requests';
import { Plugins } from '@/pages/Plugins';
import { Settings } from '@/pages/Settings';
import { Login } from '@/pages/Login';
import { api, ApiError, setToken } from '@/lib/api';
import { createT, type Lang, type MessageKey } from '@/lib/i18n';
import { useTheme, type Theme } from '@/lib/theme';
import {
  emptyMetrics,
  type GatewayConfig,
  type Metrics,
  type Plugin,
  type RequestRecord,
} from '@/lib/types';
import { cn } from '@/lib/utils';

declare const __APP_VERSION__: string;

type Page = 'overview' | 'inbound' | 'egress' | 'requests' | 'plugins' | 'settings';

const navItems: { page: Page; icon: typeof LayoutDashboard; key: MessageKey }[] = [
  { page: 'overview', icon: LayoutDashboard, key: 'nav.overview' },
  { page: 'inbound', icon: ArrowDownLeft, key: 'nav.inbound' },
  { page: 'egress', icon: ArrowUpRight, key: 'nav.egress' },
  { page: 'requests', icon: Terminal, key: 'nav.requests' },
  { page: 'plugins', icon: Code2, key: 'nav.plugins' },
  { page: 'settings', icon: Settings2, key: 'nav.settings' },
];

const themeCycle: Record<Theme, Theme> = { system: 'light', light: 'dark', dark: 'system' };
const themeIcon = { system: Monitor, light: Sun, dark: Moon } as const;

export function App() {
  const [theme, setTheme] = useTheme();
  const [lang, setLang] = useState<Lang>(() => (localStorage.getItem('affinity.lang') === 'en' ? 'en' : 'zh'));
  const t = useMemo(() => createT(lang), [lang]);
  const { toast } = useToast();
  const tRef = useRef(t);
  tRef.current = t;

  const [page, setPage] = useState<Page>('overview');
  const [query, setQuery] = useState('');
  const [mobileOpen, setMobileOpen] = useState(false);

  const [config, setConfig] = useState<GatewayConfig | null>(null);
  const [metrics, setMetrics] = useState<Metrics>(emptyMetrics);
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [requests, setRequests] = useState<RequestRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [authRequired, setAuthRequired] = useState(false);

  const load = useCallback(async (): Promise<boolean> => {
    setError('');
    const [configResult, metricsResult, pluginsResult, requestsResult] = await Promise.allSettled([
      api.config(),
      api.metrics(),
      api.plugins(),
      api.requests(),
    ]);
    if (configResult.status === 'fulfilled') {
      setConfig(configResult.value);
      setAuthRequired(false);
    } else if (configResult.reason instanceof ApiError && configResult.reason.status === 401) {
      setAuthRequired(true);
      setLoading(false);
      return false;
    } else {
      const message =
        configResult.reason instanceof Error ? configResult.reason.message : String(configResult.reason);
      setError(message);
      toast({ title: tRef.current('toast.loadError'), description: message, variant: 'error' });
    }
    if (metricsResult.status === 'fulfilled') setMetrics(metricsResult.value);
    if (pluginsResult.status === 'fulfilled') setPlugins(pluginsResult.value);
    if (requestsResult.status === 'fulfilled') setRequests(requestsResult.value);
    setLoading(false);
    return configResult.status === 'fulfilled';
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    localStorage.setItem('affinity.lang', lang);
    document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
  }, [lang]);

  const navigate = (next: Page) => {
    setPage(next);
    setMobileOpen(false);
  };

  const search = (value: string) => {
    setQuery(value);
    if (value && page !== 'requests') setPage('requests');
  };

  const counts: Partial<Record<Page, number>> = {
    inbound: config?.inbound.length ?? 0,
    egress: config?.egress.length ?? 0,
  };

  const ThemeIcon = themeIcon[theme];
  const titleKey = navItems.find((item) => item.page === page)?.key ?? 'nav.overview';

  if (authRequired) {
    return (
      <Login
        t={t}
        onSubmit={async (token) => {
          setToken(token);
          setLoading(true);
          return load();
        }}
      />
    );
  }

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      {mobileOpen ? (
        <div className="fixed inset-0 z-30 bg-black/50 lg:hidden" onClick={() => setMobileOpen(false)} />
      ) : null}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 flex w-60 shrink-0 flex-col border-r border-border bg-card px-3 py-5 transition-transform lg:static lg:translate-x-0',
          mobileOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex items-center gap-2.5 px-2 pb-8">
          <span className="grid h-8 w-8 place-items-center rounded-lg bg-primary font-black text-primary-foreground">
            A
          </span>
          <b className="text-sm font-semibold">{t('brand.name')}</b>
          <button
            className="ml-auto rounded-md p-1 text-muted-foreground hover:bg-muted lg:hidden"
            onClick={() => setMobileOpen(false)}
            aria-label={t('action.close')}
          >
            <X size={16} />
          </button>
        </div>

        <p className="px-3 pb-2 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
          {t('section.control')}
        </p>
        <nav className="flex flex-col gap-1">
          {navItems.slice(0, 4).map((item) => (
            <NavButton
              key={item.page}
              active={page === item.page}
              count={counts[item.page]}
              icon={item.icon}
              label={t(item.key)}
              onClick={() => navigate(item.page)}
            />
          ))}
        </nav>

        <p className="px-3 pb-2 pt-6 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
          {t('section.system')}
        </p>
        <nav className="flex flex-col gap-1">
          {navItems.slice(4).map((item) => (
            <NavButton
              key={item.page}
              active={page === item.page}
              icon={item.icon}
              label={t(item.key)}
              onClick={() => navigate(item.page)}
            />
          ))}
        </nav>

        <div className="mt-auto border-t border-border px-3 pt-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-2">
            <i className={cn('h-2 w-2 rounded-full', error ? 'bg-destructive' : 'bg-primary')} />
            {error ? t('status.offline') : t('status.online')}
          </span>
          <span className="mt-2 block text-[11px]">v{__APP_VERSION__} · {t('status.mode')}</span>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-border bg-background/80 px-4 backdrop-blur lg:px-6">
          <button
            className="rounded-md p-2 text-muted-foreground hover:bg-muted lg:hidden"
            onClick={() => setMobileOpen(true)}
            aria-label={t('action.menu')}
          >
            <Menu size={18} />
          </button>
          <h2 className="text-sm font-semibold">{t(titleKey)}</h2>
          <div className="ml-auto flex items-center gap-2">
            <span className="hidden items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 text-xs text-muted-foreground sm:flex">
              <Network size={13} /> {config?.listen ?? ':8236'}
            </span>
            <label className="relative hidden w-56 md:block">
              <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(event) => search(event.target.value)}
                placeholder={t('requests.searchHint')}
                className="pl-9"
              />
            </label>
            <Button variant="ghost" size="icon" onClick={() => void load()} aria-label={t('action.refresh')}>
              <RefreshCw size={16} />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setTheme(themeCycle[theme])}
              aria-label={t('settings.theme')}
              title={t(`settings.theme.${theme}` as MessageKey)}
            >
              <ThemeIcon size={16} />
            </Button>
          </div>
        </header>

        <main className="min-w-0 flex-1">
          {error && !config ? (
            <div className="mx-auto flex max-w-md flex-col items-center gap-3 px-6 py-24 text-center">
              <h2 className="text-base font-semibold">{t('error.load')}</h2>
              <p className="text-sm text-muted-foreground">{t('error.loadHint')}</p>
              <p className="font-mono text-xs text-destructive">{error}</p>
              <Button onClick={() => void load()}>
                <RefreshCw size={15} /> {t('action.retry')}
              </Button>
            </div>
          ) : loading ? (
            <p className="px-6 py-16 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : page === 'overview' ? (
            <Overview
              t={t}
              metrics={metrics}
              requests={requests}
              config={config}
              onViewRequests={() => navigate('requests')}
            />
          ) : page === 'inbound' ? (
            <RoutesPage direction="inbound" routes={config?.inbound ?? []} plugins={plugins} t={t} onReload={load} />
          ) : page === 'egress' ? (
            <RoutesPage direction="egress" routes={config?.egress ?? []} plugins={plugins} t={t} onReload={load} />
          ) : page === 'requests' ? (
            <Requests t={t} requests={requests} query={query} onQueryChange={setQuery} />
          ) : page === 'plugins' ? (
            <Plugins t={t} plugins={plugins} onReload={load} />
          ) : (
            <Settings
              t={t}
              theme={theme}
              onThemeChange={setTheme}
              lang={lang}
              onLangChange={setLang}
              config={config}
            />
          )}
        </main>
      </div>
    </div>
  );
}

function NavButton({
  active,
  icon: Icon,
  label,
  count,
  onClick,
}: {
  active: boolean;
  icon: typeof LayoutDashboard;
  label: string;
  count?: number;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors',
        active ? 'bg-muted font-medium text-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
      )}
    >
      <Icon size={16} />
      <span>{label}</span>
      {count !== undefined ? (
        <span className="ml-auto rounded-full bg-background px-2 text-[11px] text-muted-foreground">{count}</span>
      ) : null}
    </button>
  );
}
