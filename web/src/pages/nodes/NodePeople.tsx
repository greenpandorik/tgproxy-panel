import { useTranslation } from 'react-i18next';

import { isMetricPresent } from '@/components/common/metric';
import { formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

import { DASH } from './nodeDisplay';

import type { Node } from '@/api/types';

/** People online on a server with its connections under them; a dash while nothing fresh is known. */
export function NodePeople({ node, className }: { node: Pick<Node, 'people_online' | 'connections'>; className?: string }) {
  const { t, i18n } = useTranslation();
  const num = (v: number) => formatNumber(v, i18n.language);
  if (!isMetricPresent(node.people_online) && !isMetricPresent(node.connections)) {
    return <span className={cn('mono block text-mono text-dim', className)}>{DASH}</span>;
  }
  return (
    <span className={cn('block', className)}>
      <span className={cn('mono block text-mono', isMetricPresent(node.people_online) ? 'text-foreground' : 'text-dim')}>
        {isMetricPresent(node.people_online) ? t('common.approx', { value: num(node.people_online) }) : DASH}
      </span>
      {isMetricPresent(node.connections) && (
        <span className="mono block text-micro text-mute">{t('common.connections_count', { value: num(node.connections) })}</span>
      )}
    </span>
  );
}
