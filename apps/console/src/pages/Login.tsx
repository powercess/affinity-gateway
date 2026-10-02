import { useState, type FormEvent } from 'react';
import { KeyRound } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import type { Translate } from '@/lib/i18n';

type Props = {
  t: Translate;
  onSubmit: (token: string) => Promise<boolean>;
};

export function Login({ t, onSubmit }: Props) {
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!token.trim()) return;
    setBusy(true);
    setError('');
    const ok = await onSubmit(token.trim());
    setBusy(false);
    if (!ok) setError(t('auth.invalid'));
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm p-6">
        <div className="mb-4 flex items-center gap-3">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary text-primary-foreground">
            <KeyRound size={18} />
          </span>
          <div>
            <h1 className="text-sm font-semibold">{t('auth.title')}</h1>
            <p className="text-xs text-muted-foreground">{t('auth.hint')}</p>
          </div>
        </div>
        <form className="flex flex-col gap-3" onSubmit={submit}>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="access-token">{t('auth.token')}</Label>
            <Input
              id="access-token"
              type="password"
              value={token}
              autoFocus
              autoComplete="off"
              onChange={(event) => setToken(event.target.value)}
            />
          </div>
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
          <Button type="submit" disabled={busy || !token.trim()}>
            {busy ? t('common.loading') : t('auth.submit')}
          </Button>
        </form>
      </Card>
    </div>
  );
}
