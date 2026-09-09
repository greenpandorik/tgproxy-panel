import { Monitor, Moon, Sun, SlidersHorizontal } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { useTheme } from '@/theme/ThemeProvider';
import { setLang } from '@/i18n';

export function PreferencesForm() {
  const { t, i18n } = useTranslation();
  const { preference, setTheme, density, setDensity, collapsed, setCollapsed, resetPreferences } = useTheme();
  return (
    <Panel>
      <PanelHeader icon={SlidersHorizontal} title={t('preferences.title')} />
      <PanelBody className="space-y-6">
        <p className="max-w-prose text-body text-mute">{t('preferences.description')}</p>
        <div className="grid gap-6 md:grid-cols-2 xl:grid-cols-4">
          <fieldset className="min-w-0 space-y-3">
            <legend className="text-body font-medium">{t('preferences.theme')}</legend>
            <div className="flex flex-wrap gap-2">
              {(
                [
                  { value: 'light', icon: Sun },
                  { value: 'dark', icon: Moon },
                  { value: 'system', icon: Monitor },
                ] as const
              ).map(({ value, icon: Icon }) => (
                <Button
                  key={value}
                  variant={preference === value ? 'secondary' : 'outline'}
                  aria-pressed={preference === value}
                  onClick={() => setTheme(value)}
                >
                  <Icon />
                  {t(`preferences.${value}`)}
                </Button>
              ))}
            </div>
          </fieldset>
          <fieldset className="min-w-0 space-y-3">
            <legend className="text-body font-medium">{t('preferences.density')}</legend>
            <div className="flex flex-wrap gap-2">
              {(['comfortable', 'compact'] as const).map((value) => (
                <Button
                  key={value}
                  variant={density === value ? 'secondary' : 'outline'}
                  aria-pressed={density === value}
                  onClick={() => setDensity(value)}
                >
                  {t(`preferences.${value}`)}
                </Button>
              ))}
            </div>
          </fieldset>
          <fieldset className="min-w-0 space-y-3">
            <legend className="text-body font-medium">{t('preferences.sidebar')}</legend>
            <div className="flex flex-wrap gap-2">
              {([false, true] as const).map((value) => (
                <Button
                  key={String(value)}
                  variant={collapsed === value ? 'secondary' : 'outline'}
                  aria-pressed={collapsed === value}
                  onClick={() => setCollapsed(value)}
                >
                  {t(value ? 'preferences.collapsed' : 'preferences.expanded')}
                </Button>
              ))}
            </div>
          </fieldset>
          <fieldset className="min-w-0 space-y-3">
            <legend className="text-body font-medium">{t('common.language')}</legend>
            <div className="flex gap-2">
              {(['ru', 'en'] as const).map((value) => (
                <Button
                  key={value}
                  variant={i18n.language.startsWith(value) ? 'secondary' : 'outline'}
                  aria-pressed={i18n.language.startsWith(value)}
                  onClick={() => setLang(value)}
                >
                  {value === 'ru' ? 'Русский' : 'English'}
                </Button>
              ))}
            </div>
          </fieldset>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-hairline pt-4">
          <span role="status" className="text-label text-mute">
            {t('preferences.autosaved')}
          </span>
          <Button variant="outline" onClick={resetPreferences}>
            {t('preferences.reset')}
          </Button>
        </div>
      </PanelBody>
    </Panel>
  );
}
