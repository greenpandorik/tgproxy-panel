import { Monitor, Moon, Sun, SlidersHorizontal } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { useTheme } from '@/theme/ThemeProvider';
import { setLang } from '@/i18n';

import type { ReactNode } from 'react';

function Choice({ label, children }: { label: string; children: ReactNode }) {
  const id = useId();
  return (
    <div role="group" aria-labelledby={id} className="grid gap-2 sm:grid-cols-[11rem_minmax(0,1fr)] sm:items-center sm:gap-4">
      <span id={id} className="text-body font-medium">
        {label}
      </span>
      <div className="flex flex-wrap gap-2">{children}</div>
    </div>
  );
}

export function PreferencesForm() {
  const { t, i18n } = useTranslation();
  const { preference, setTheme, density, setDensity, collapsed, setCollapsed, resetPreferences } = useTheme();
  return (
    <Panel className="@container">
      <PanelHeader icon={SlidersHorizontal} title={t('preferences.title')} />
      <PanelBody className="grid gap-5 @min-[56rem]:grid-cols-2 @min-[56rem]:gap-x-10">
        <Choice label={t('preferences.theme')}>
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
        </Choice>
        <Choice label={t('preferences.density')}>
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
        </Choice>
        <Choice label={t('preferences.sidebar')}>
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
        </Choice>
        <Choice label={t('common.language')}>
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
        </Choice>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-hairline pt-4 @min-[56rem]:col-span-2">
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
