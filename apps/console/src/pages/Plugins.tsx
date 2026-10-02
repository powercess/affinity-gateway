import { useEffect, useMemo, useState } from 'react';
import { Code2, Plus, Save, Trash2 } from 'lucide-react';
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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { api } from '@/lib/api';
import type { MessageKey, Translate } from '@/lib/i18n';
import { emptyPlugin, type Plugin } from '@/lib/types';

const directionKey: Record<Plugin['direction'], MessageKey> = {
  inbound: 'plugins.direction.inbound',
  egress: 'plugins.direction.egress',
  both: 'plugins.direction.both',
};

type Props = {
  t: Translate;
  plugins: Plugin[];
  onReload: () => void;
};

export function Plugins({ t, plugins, onReload }: Props) {
  const { toast } = useToast();
  const [selectedId, setSelectedId] = useState<string | null>(plugins[0]?.id ?? null);
  const [draft, setDraft] = useState<Plugin>(() => plugins[0] ?? emptyPlugin());
  const [isNew, setIsNew] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState(false);

  useEffect(() => {
    if (isNew) return;
    if (!plugins.length) {
      setSelectedId(null);
      setDraft(emptyPlugin());
      return;
    }
    const current = plugins.find((plugin) => plugin.id === selectedId) ?? plugins[0];
    setSelectedId(current.id);
    setDraft(current);
  }, [plugins, selectedId, isNew]);

  const sorted = useMemo(() => [...plugins].sort((a, b) => a.id.localeCompare(b.id)), [plugins]);

  const select = (plugin: Plugin) => {
    setIsNew(false);
    setSelectedId(plugin.id);
    setDraft(plugin);
    setError('');
  };

  const startNew = () => {
    setIsNew(true);
    setSelectedId(null);
    setDraft(emptyPlugin());
    setError('');
  };

  const save = async () => {
    if (!draft.id.trim()) {
      setError(t('common.required'));
      return;
    }
    setSaving(true);
    setError('');
    try {
      await api.savePlugin({ ...draft, id: draft.id.trim() });
      toast({ title: t('plugins.saved'), variant: 'success' });
      setIsNew(false);
      setSelectedId(draft.id.trim());
      onReload();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      toast({ title: t('plugins.saveFailed'), description: message, variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!draft.id) return;
    try {
      await api.deletePlugin(draft.id);
      toast({ title: t('plugins.deleted'), variant: 'success' });
      setPendingDelete(false);
      setIsNew(false);
      setSelectedId(null);
      onReload();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setPendingDelete(false);
      toast({ title: t('plugins.deleteFailed'), description: message, variant: 'error' });
    }
  };

  return (
    <section className="mx-auto w-full max-w-6xl px-6 py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">{t('plugins.title')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('plugins.subtitle')}</p>
        </div>
        <Button onClick={startNew}>
          <Plus size={16} /> {t('plugins.new')}
        </Button>
      </header>

      <div className="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
        <Card className="h-fit p-2">
          {sorted.length === 0 ? (
            <p className="px-3 py-6 text-center text-xs text-muted-foreground">{t('plugins.empty')}</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {sorted.map((plugin) => (
                <li key={plugin.id}>
                  <button
                    onClick={() => select(plugin)}
                    className={
                      'flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left transition-colors ' +
                      (selectedId === plugin.id && !isNew
                        ? 'bg-muted text-foreground'
                        : 'text-muted-foreground hover:bg-muted hover:text-foreground')
                    }
                  >
                    <Code2 size={15} />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{plugin.id}</span>
                    <Badge variant="outline">{t(directionKey[plugin.direction])}</Badge>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card className="flex flex-col gap-4 p-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="plugin-id">{t('plugins.id')}</Label>
              <Input
                id="plugin-id"
                value={draft.id}
                disabled={!isNew}
                onChange={(event) => setDraft({ ...draft, id: event.target.value })}
                placeholder="opencode.session"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="plugin-direction">{t('plugins.direction')}</Label>
              <Select
                id="plugin-direction"
                value={draft.direction}
                onChange={(event) => setDraft({ ...draft, direction: event.target.value as Plugin['direction'] })}
              >
                <option value="inbound">{t('plugins.direction.inbound')}</option>
                <option value="egress">{t('plugins.direction.egress')}</option>
                <option value="both">{t('plugins.direction.both')}</option>
              </Select>
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="plugin-description">{t('plugins.description')}</Label>
            <Input
              id="plugin-description"
              value={draft.description}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="plugin-source">{t('plugins.source')}</Label>
            <textarea
              id="plugin-source"
              value={draft.source}
              spellCheck={false}
              onChange={(event) => setDraft({ ...draft, source: event.target.value })}
              className="h-72 w-full resize-y rounded-md border border-input bg-card p-3 font-mono text-xs leading-relaxed text-foreground outline-none"
            />
          </div>

          {error ? <p className="text-xs text-destructive">{error}</p> : null}

          <div className="flex items-center gap-2">
            <Button onClick={save} disabled={saving}>
              <Save size={15} /> {saving ? t('common.loading') : t('plugins.save')}
            </Button>
            {!isNew && draft.id ? (
              <Button
                variant="ghost"
                className="ml-auto text-destructive hover:bg-destructive/10"
                onClick={() => setPendingDelete(true)}
              >
                <Trash2 size={14} /> {t('plugins.delete')}
              </Button>
            ) : null}
          </div>

          <p className="text-[11px] text-muted-foreground">{t('plugins.runtimeNote')}</p>
        </Card>
      </div>

      <Dialog open={pendingDelete} onOpenChange={(open) => !open && setPendingDelete(false)}>
        <DialogContent className="w-[min(420px,calc(100vw-2rem))]">
          <DialogHeader>
            <DialogTitle>{t('plugins.delete')}</DialogTitle>
            <DialogDescription>{t('plugins.deleteConfirm', { name: draft.id })}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDelete(false)}>
              {t('action.cancel')}
            </Button>
            <Button variant="destructive" onClick={remove}>
              {t('action.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
