import { LIMIT_FIELD_NAMES } from './LimitsFields';

import type { ProfileLimits } from '@/api/types';
import type { TelemtLimitsForm } from '@/lib/units';

/** How many limits the operator has actually set, across both blocks. */
export function countLimits(limits: ProfileLimits, telemt: TelemtLimitsForm): number {
  const profile = LIMIT_FIELD_NAMES.filter((name) => Number(limits[name] ?? 0) > 0).length;
  const perUser = Object.values(telemt).filter((raw) => raw.trim() !== '').length;
  return profile + perUser;
}
