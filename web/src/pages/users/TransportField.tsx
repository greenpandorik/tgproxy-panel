import { useTranslation } from 'react-i18next';

import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { HelpButton } from '@/help';

import { CARRIER_MODES, carrierSlug } from './transport';

import type { TransportScope } from './transport';
import type { CarrierMode } from '@/api/types';

interface TransportFieldProps {
  scope: TransportScope;
  value: CarrierMode;
  onChange: (next: CarrierMode) => void;
}

/** The carrier mode where it is a real choice, and the automatic-transport notice where it is not. */
export function TransportField({ scope, value, onChange }: TransportFieldProps) {
  const { t } = useTranslation();

  if (scope === 'none') return null;

  if (scope === 'telemt') {
    return (
      <div className="space-y-2" data-transport="auto">
        <div className="flex items-center gap-2">
          <Label>{t('keys.field_transport')}</Label>
          <HelpButton topic="keys.transport" className="-my-1" />
        </div>
        <div className="rounded-control border border-hairline px-3 py-2">
          <p className="text-body text-foreground">{t('keys.transport_auto')}</p>
          <p className="mt-1 text-label text-mute">{t('keys.transport_auto_hint')}</p>
        </div>
      </div>
    );
  }

  const legacy = scope === 'mixed';

  return (
    <div className="space-y-2" data-transport={legacy ? 'legacy' : 'carrier'}>
      <div className="flex items-center gap-2">
        <Label htmlFor="key-carrier">{legacy ? t('keys.field_carrier_mode_legacy') : t('keys.field_carrier_mode')}</Label>
        {legacy && <HelpButton topic="keys.transport" className="-my-1" />}
      </div>
      {legacy && <p className="text-label text-mute">{t('keys.field_carrier_mode_legacy_hint')}</p>}
      <Select value={value} onValueChange={(v) => onChange(v as CarrierMode)}>
        <SelectTrigger id="key-carrier" className="w-full">
          {/* Resolve the label explicitly - SelectValue only reflects a matched item's
              rendered label once the popup has mounted at least once, so it would
              otherwise show the raw value ("https-lanes"). */}
          <SelectValue>{(v: CarrierMode) => t(`keys.carrier_${carrierSlug(v ?? 'https')}`)}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {CARRIER_MODES.map((m) => (
            <SelectItem key={m} value={m}>
              {t(`keys.carrier_${carrierSlug(m)}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-label text-mute">{t(`keys.carrier_${carrierSlug(value)}_desc`)}</p>
    </div>
  );
}
