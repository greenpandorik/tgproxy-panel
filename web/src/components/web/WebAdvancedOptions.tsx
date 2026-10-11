import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import type { WebPolicy } from './webPolicy';

export function WebAdvancedOptions({
  policy,
  supported,
  change,
}: {
  policy: WebPolicy;
  supported: boolean;
  change: (patch: Partial<WebPolicy>) => void;
}) {
  const { t } = useTranslation();
  return (
    <fieldset disabled={!supported} className="space-y-4 border-t border-hairline pt-4 sm:col-span-2 xl:col-span-3">
      <legend className="text-label font-medium">{t('web.http_options')}</legend>
      {!supported && <p className="max-w-[72ch] text-label text-mute">{t('web.advanced_requires_update')}</p>}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <div className="space-y-2">
          <Label htmlFor="web-http-method">{t('web.http_method')}</Label>
          <select
            id="web-http-method"
            className="ops-select"
            value={policy.carrier_method ?? 'unmanaged'}
            onChange={(e) =>
              change({ carrier_method: e.target.value === 'unmanaged' ? null : (e.target.value as 'post' | 'put') })
            }
          >
            <option value="unmanaged">{t('web.setting_unmanaged')}</option>
            <option value="post">POST</option>
            <option value="put">PUT</option>
          </select>
          <p className="text-micro text-mute">{t('web.http_method_hint')}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="web-conveyor">{t('web.conveyor')}</Label>
          <select
            id="web-conveyor"
            className="ops-select"
            value={policy.conveyor == null ? 'unmanaged' : String(policy.conveyor)}
            onChange={(e) => change({ conveyor: e.target.value === 'unmanaged' ? null : e.target.value === 'true' })}
          >
            <option value="unmanaged">{t('web.setting_unmanaged')}</option>
            <option value="true">{t('web.state_on')}</option>
            <option value="false">{t('web.state_off')}</option>
          </select>
          <p className="text-micro text-mute">{t('web.conveyor_hint')}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="web-decoy-resolve">{t('web.decoy_resolve')}</Label>
          <select
            id="web-decoy-resolve"
            className="ops-select"
            value={policy.decoy_resolve ?? 'unmanaged'}
            onChange={(e) =>
              change({ decoy_resolve: e.target.value === 'unmanaged' ? null : (e.target.value as 'never' | 'startup') })
            }
          >
            <option value="unmanaged">{t('web.setting_unmanaged')}</option>
            <option value="never">{t('web.decoy_resolve_never')}</option>
            <option value="startup">{t('web.decoy_resolve_startup')}</option>
          </select>
          <p className="text-micro text-mute">{t('web.decoy_resolve_hint')}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="web-path-mode">{t('web.path_mode')}</Label>
          <select
            id="web-path-mode"
            className="ops-select"
            value={policy.base_path == null ? 'unmanaged' : 'prefix'}
            onChange={(e) => change({ base_path: e.target.value === 'unmanaged' ? null : (policy.base_path ?? '') })}
          >
            <option value="unmanaged">{t('web.setting_unmanaged')}</option>
            <option value="prefix">{t('web.path_manage')}</option>
          </select>
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="web-base-path">{t('web.base_path')}</Label>
          <Input
            id="web-base-path"
            value={policy.base_path ?? ''}
            disabled={policy.base_path == null}
            placeholder="relay/telegram"
            maxLength={128}
            pattern="[A-Za-z0-9][A-Za-z0-9_-]*(/[A-Za-z0-9][A-Za-z0-9_-]*)*"
            onChange={(e) => change({ base_path: e.target.value })}
          />
          <p className="max-w-[72ch] text-micro text-mute">{t('web.base_path_hint')}</p>
        </div>
      </div>
    </fieldset>
  );
}
