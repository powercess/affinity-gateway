import { Monitor, Moon, Sun } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { Select } from '@/components/ui/select';
import { Label } from '@/components/ui/label';
import type { Lang, Translate } from '@/lib/i18n';
import type { GatewayConfig } from '@/lib/types';
import type { Theme } from '@/lib/theme';
import { cn } from '@/lib/utils';

type Props = {
  t: Translate;
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
  lang: Lang;
  onLangChange: (lang: Lang) => void;
  config: GatewayConfig | null;
};

const themeOptions: { value: Theme; icon: typeof Sun; key: 'settings.theme.system' | 'settings.theme.light' | 'settings.theme.dark' }[] = [
  { value: 'system', icon: Monitor, key: 'settings.theme.system' },
  { value: 'light', icon: Sun, key: 'settings.theme.light' },
  { value: 'dark', icon: Moon, key: 'settings.theme.dark' },
];

export function Settings({ t, theme, onThemeChange, lang, onLangChange, config }: Props) {
  return (
    <section className="mx-auto w-full max-w-3xl px-6 py-8">
      <header className="mb-6">
        <h1 className="text-xl font-semibold">{t('settings.title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('settings.subtitle')}</p>
      </header>

      <div className="flex flex-col gap-4">
        <Card className="p-5">
          <h3 className="text-sm font-semibold">{t('settings.appearance')}</h3>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label>{t('settings.theme')}</Label>
              <div className="grid grid-cols-3 gap-2">
                {themeOptions.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    onClick={() => onThemeChange(option.value)}
                    className={cn(
                      'flex flex-col items-center gap-1.5 rounded-lg border px-3 py-3 text-xs transition-colors',
                      theme === option.value
                        ? 'border-primary bg-accent text-accent-foreground'
                        : 'border-border bg-card text-muted-foreground hover:bg-muted',
                    )}
                  >
                    <option.icon size={16} />
                    {t(option.key)}
                  </button>
                ))}
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="language">{t('settings.language')}</Label>
              <Select id="language" value={lang} onChange={(event) => onLangChange(event.target.value as Lang)}>
                <option value="zh">中文</option>
                <option value="en">English</option>
              </Select>
            </div>
          </div>
        </Card>

        <Card className="p-5">
          <h3 className="text-sm font-semibold">{t('settings.gateway')}</h3>
          <dl className="mt-4 grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <dt className="text-xs text-muted-foreground">{t('settings.listen')}</dt>
              <dd className="font-mono text-sm">{config?.listen ?? ':8236'}</dd>
              <p className="text-[11px] text-muted-foreground">{t('settings.listenHint')}</p>
            </div>
            <div className="flex flex-col gap-1">
              <dt className="text-xs text-muted-foreground">{t('settings.mode')}</dt>
              <dd className="text-sm">{t('status.mode')}</dd>
            </div>
            <div className="flex flex-col gap-1">
              <dt className="text-xs text-muted-foreground">{t('settings.version')}</dt>
              <dd className="text-sm">v{config?.version ?? 1}</dd>
            </div>
          </dl>
        </Card>
      </div>
    </section>
  );
}
