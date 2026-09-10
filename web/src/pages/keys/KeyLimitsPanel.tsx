import { ChevronLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { Button } from '@/components/ui/button';
import { HelpButton } from '@/help';

import { LIMIT_FIELD_NAMES, LimitsFields } from './LimitsFields';
import { TelemtLimitsFields } from './TelemtLimitsFields';

import type { LimitFieldName } from './LimitsFields';
import type { ProfileLimits } from '@/api/types';
import type { TelemtLimitsForm } from '@/lib/units';

interface KeyLimitsPanelProps {
  limits: ProfileLimits;
  onLimitsChange: (next: ProfileLimits) => void;
  telemt: TelemtLimitsForm;
  onTelemtChange: (next: TelemtLimitsForm) => void;
  /** False when no telemt node is selected: the per-user limits have nothing to enforce them. */
  telemtAvailable: boolean;
  legacyAvailable: boolean;
  limitsErrors: Partial<Record<LimitFieldName, string>>;
  telemtErrors: Partial<Record<keyof TelemtLimitsForm, string>>;
  onBack: () => void;
}

/** The detailed limits, on their own screen so the create form stays a short list of decisions. */
export function KeyLimitsPanel({
  limits,
  onLimitsChange,
  telemt,
  onTelemtChange,
  telemtAvailable,
  legacyAvailable,
  limitsErrors,
  telemtErrors,
  onBack,
}: KeyLimitsPanelProps) {
  const { t } = useTranslation();
  const advancedCount = LIMIT_FIELD_NAMES.filter((name) => Number(limits[name] ?? 0) > 0).length;

  return (
    <div className="space-y-4 py-1" data-slot="key-limits-panel">
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <Button type="button" variant="ghost" size="icon-sm" className="-ml-2 text-mute" onClick={onBack}>
            <ChevronLeft />
            <span className="sr-only">{t('keys.limits_edit_back')}</span>
          </Button>
          <h3 className="text-body font-medium text-foreground">{t('keys.limits_edit_title')}</h3>
          <HelpButton topic="keys.limits" className="-my-1" />
        </div>
        <p className="text-label text-mute">{t('keys.limits_edit_description')}</p>
      </div>

      <TelemtLimitsFields value={telemt} onChange={onTelemtChange} disabled={!telemtAvailable} errors={telemtErrors} />

      <AdvancedSettings label={t('keys.field_limits_toggle')} defaultOpen={advancedCount > 0}>
        <p className="mb-3 text-label text-mute">{t(telemtAvailable && !legacyAvailable ? 'keys.web_limits_hint' : 'keys.legacy_limits_hint')}</p>
        <LimitsFields value={limits} onChange={onLimitsChange} errors={limitsErrors} telemtOnly={telemtAvailable && !legacyAvailable} />
      </AdvancedSettings>
    </div>
  );
}
