import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Check } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';
import { emptyRoute, type Direction, type Plugin, type Route } from '@/lib/types';
import type { Translate } from '@/lib/i18n';
import { cn } from '@/lib/utils';

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  direction: Direction;
  initial: Route | null;
  plugins: Plugin[];
  suggestedPath?: string;
  t: Translate;
  onSubmit: (route: Route) => Promise<void>;
};

export function RouteDialog({ open, onOpenChange, direction, initial, plugins, suggestedPath, t, onSubmit }: Props) {
  const [form, setForm] = useState<Route>(() => initial ?? emptyRoute(direction));
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setForm(initial ?? { ...emptyRoute(direction), ...(suggestedPath ? { path: suggestedPath } : {}) });
      setError('');
    }
  }, [open, initial, direction, suggestedPath]);

  const available = useMemo(
    () => plugins.filter((plugin) => plugin.direction === 'both' || plugin.direction === direction),
    [plugins, direction],
  );

  const update = <K extends keyof Route>(key: K, value: Route[K]) =>
    setForm((current) => ({ ...current, [key]: value }));

  const togglePlugin = (id: string) =>
    setForm((current) => ({
      ...current,
      plugins: current.plugins.includes(id)
        ? current.plugins.filter((item) => item !== id)
        : [...current.plugins, id],
    }));

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!form.name.trim()) {
      setError(t('common.required'));
      return;
    }
    if (!form.target.trim()) {
      setError(t('common.required'));
      return;
    }
    setSaving(true);
    setError('');
    try {
      await onSubmit({ ...form, id: form.id.trim(), name: form.name.trim(), target: form.target.trim() });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial ? t('routes.editTitle') : t('routes.createTitle')}</DialogTitle>
          <DialogDescription>{direction === 'inbound' ? t('inbound.subtitle') : t('egress.subtitle')}</DialogDescription>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={submit}>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="route-name">{t('routes.form.name')}</Label>
              <Input
                id="route-name"
                value={form.name}
                onChange={(event) => update('name', event.target.value)}
                placeholder="Chat Completions"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="route-id">{t('routes.form.id')}</Label>
              <Input
                id="route-id"
                value={form.id}
                disabled={Boolean(initial)}
                onChange={(event) => update('id', event.target.value)}
                placeholder="chat"
              />
            </div>
          </div>
          <p className="-mt-2 text-[11px] text-muted-foreground">{t('routes.form.idHint')}</p>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="route-path">{direction === 'inbound' ? t('routes.form.prefix') : t('routes.path')}</Label>
            {direction === 'inbound' ? (
              <>
                <Input
                  id="route-path"
                  value={form.path}
                  onChange={(event) => update('path', event.target.value)}
                  placeholder="/site1"
                />
                <p className="text-[11px] text-muted-foreground">{t('routes.form.prefixHint')}</p>
              </>
            ) : (
              <Input id="route-path" value={`/egress/${form.id || '{id}'}`} readOnly className="opacity-70" />
            )}
            {direction === 'egress' && (
              <p className="text-[11px] text-muted-foreground">{t('routes.form.pathLocked')}</p>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="route-target">{t('routes.form.target')}</Label>
            <Input
              id="route-target"
              value={form.target}
              onChange={(event) => update('target', event.target.value)}
              placeholder="https://api.example.com"
            />
            <p className="text-[11px] text-muted-foreground">{t('routes.form.targetHint')}</p>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="route-state">{t('routes.form.state')}</Label>
            <Select
              id="route-state"
              value={form.state}
              onChange={(event) => update('state', event.target.value as Route['state'])}
            >
              <option value="active">{t('common.active')}</option>
              <option value="inactive">{t('common.inactive')}</option>
            </Select>
          </div>

          <div className="flex flex-col gap-2">
            <Label>{t('routes.form.plugins')}</Label>
            {available.length === 0 ? (
              <p className="text-[11px] text-muted-foreground">{t('routes.form.pluginsEmpty')}</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {available.map((plugin) => {
                  const index = form.plugins.indexOf(plugin.id);
                  const selected = index >= 0;
                  return (
                    <button
                      type="button"
                      key={plugin.id}
                      onClick={() => togglePlugin(plugin.id)}
                      title={plugin.description}
                      className={cn(
                        'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs transition-colors',
                        selected
                          ? 'border-transparent bg-primary text-primary-foreground'
                          : 'border-border bg-card text-muted-foreground hover:bg-muted',
                      )}
                    >
                      {selected ? <Check size={12} /> : null}
                      {plugin.id}
                      {selected ? <span className="opacity-70">{index + 1}</span> : null}
                    </button>
                  );
                })}
              </div>
            )}
            <p className="text-[11px] text-muted-foreground">{t('routes.form.pluginsHint')}</p>
          </div>

          {error ? <p className="text-xs text-destructive">{error}</p> : null}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('action.cancel')}
            </Button>
            <Button type="submit" disabled={saving}>
              {saving ? t('common.loading') : t('action.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
