import { useMemo, useState } from 'react';
import { Network, Pencil, Plus, Power, SlidersHorizontal, Trash2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { RouteDialog } from '@/components/RouteDialog';
import { useToast } from '@/components/ui/toast';
import { api } from '@/lib/api';
import type { Translate } from '@/lib/i18n';
import type { Direction, Plugin, Route } from '@/lib/types';
import { cn } from '@/lib/utils';

type Props = {
  direction: Direction;
  routes: Route[];
  plugins: Plugin[];
  t: Translate;
  onReload: () => void;
};

export function RoutesPage({ direction, routes, plugins, t, onReload }: Props) {
  const [editing, setEditing] = useState<Route | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Route | null>(null);
  const { toast } = useToast();

  const title = direction === 'inbound' ? t('inbound.title') : t('egress.title');
  const subtitle = direction === 'inbound' ? t('inbound.subtitle') : t('egress.subtitle');
  const targetLabel = direction === 'inbound' ? t('routes.target') : t('routes.origin');

  const suggestedPath = useMemo(() => {
    if (direction !== 'inbound') return undefined;
    const taken = new Set(routes.map((route) => route.path));
    const base = '/v1/chat/completions';
    if (!taken.has(base)) return base;
    let index = 2;
    while (taken.has(`/v1/route-${index}`)) index += 1;
    return `/v1/route-${index}`;
  }, [direction, routes]);

  const openCreate = () => {
    setEditing(null);
    setDialogOpen(true);
  };

  const openEdit = (route: Route) => {
    setEditing(route);
    setDialogOpen(true);
  };

  const submit = async (route: Route) => {
    try {
      if (editing) {
        await api.updateRoute(direction, editing.id, route);
        toast({ title: t('toast.updateSuccess'), variant: 'success' });
      } else {
        await api.createRoute(direction, route);
        toast({ title: t('toast.createSuccess'), variant: 'success' });
      }
      onReload();
    } catch (err) {
      toast({ title: t('toast.failure'), description: messageOf(err), variant: 'error' });
      throw err;
    }
  };

  const toggleState = async (route: Route) => {
    try {
      await api.updateRoute(direction, route.id, {
        ...route,
        state: route.state === 'active' ? 'inactive' : 'active',
      });
      toast({ title: t('toast.toggleSuccess'), variant: 'success' });
      onReload();
    } catch (err) {
      toast({ title: t('toast.failure'), description: messageOf(err), variant: 'error' });
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await api.deleteRoute(direction, pendingDelete.id);
      toast({ title: t('toast.deleteSuccess'), variant: 'success' });
      setPendingDelete(null);
      onReload();
    } catch (err) {
      toast({ title: t('toast.failure'), description: messageOf(err), variant: 'error' });
    }
  };

  return (
    <section className="mx-auto w-full max-w-6xl px-6 py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">{title}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
        </div>
        <Button onClick={openCreate}>
          <Plus size={16} /> {t('action.add')}
        </Button>
      </header>

      {routes.length === 0 ? (
        <Card className="flex flex-col items-center gap-2 px-6 py-16 text-center">
          <Network size={28} className="text-muted-foreground" />
          <h3 className="text-sm font-medium">{t('routes.empty')}</h3>
          <p className="text-xs text-muted-foreground">{t('routes.emptyHint')}</p>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {routes.map((route) => (
            <Card key={route.id} className="flex flex-col gap-3 p-5">
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                  <span
                    className={cn(
                      'grid h-9 w-9 place-items-center rounded-lg',
                      direction === 'inbound'
                        ? 'bg-accent text-accent-foreground'
                        : 'bg-primary/15 text-primary',
                    )}
                  >
                    <Network size={18} />
                  </span>
                  <div>
                    <h3 className="text-sm font-semibold">{route.name}</h3>
                    <code className="text-[11px] text-muted-foreground">
                      {direction === 'inbound' ? `/v1/ · ${t('routes.singleTarget')}` : route.path}
                    </code>
                  </div>
                </div>
                <Badge variant={route.state === 'active' ? 'success' : 'secondary'}>
                  {route.state === 'active' ? t('common.enabled') : t('common.disabled')}
                </Badge>
              </div>

              <div>
                <p className="text-[11px] text-muted-foreground">{targetLabel}</p>
                <p className="truncate text-sm">{route.target}</p>
              </div>

              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <SlidersHorizontal size={13} />
                {route.plugins?.length
                  ? `${t('routes.plugins')}: ${route.plugins.join(' → ')}`
                  : t('routes.pluginCount', { n: 0 })}
              </div>

              <div className="mt-1 flex items-center gap-2 border-t border-border pt-3">
                <Button size="sm" variant="secondary" onClick={() => openEdit(route)}>
                  <Pencil size={13} /> {t('action.edit')}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => toggleState(route)}>
                  <Power size={13} /> {route.state === 'active' ? t('common.inactive') : t('common.active')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="ml-auto text-destructive hover:bg-destructive/10"
                  onClick={() => setPendingDelete(route)}
                >
                  <Trash2 size={13} /> {t('action.delete')}
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <RouteDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        direction={direction}
        initial={editing}
        plugins={plugins}
        suggestedPath={suggestedPath}
        t={t}
        onSubmit={submit}
      />

      <Dialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <DialogContent className="w-[min(420px,calc(100vw-2rem))]">
          <DialogHeader>
            <DialogTitle>{t('routes.deleteTitle')}</DialogTitle>
            <DialogDescription>
              {t('routes.deleteConfirm', { name: pendingDelete?.name ?? '' })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDelete(null)}>
              {t('action.cancel')}
            </Button>
            <Button variant="destructive" onClick={confirmDelete}>
              {t('action.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
