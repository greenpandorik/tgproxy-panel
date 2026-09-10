import { Radar } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { useFleetWebCarriers } from '@/api/web';
import { ErrorState } from '@/components/common/ErrorState';
import { Panel, PanelBody, PanelHeader } from '@/components/common/Panel';
import { CapabilityNotice } from '@/components/web/CapabilityNotice';
import { CarrierDistribution } from '@/components/web/CarrierDistribution';
import { WebCounters } from '@/components/web/WebCounters';
import { webCapabilityCounts } from '@/components/web/capability';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api';
import { formatNumber } from '@/lib/format';

import type { Node } from '@/api/types';

/** The whole fleet's carrier selections over the last day, with the same absence rules as one node. */
export function FleetCarriersCard({ nodes }: { nodes: Node[] }) {
  const { t, i18n } = useTranslation();
  const carriersQuery = useFleetWebCarriers();
  const stats = carriersQuery.data;
  const caps = webCapabilityCounts(nodes);

  return (
    <Panel>
      <PanelHeader
        icon={Radar}
        title={t('web.fleet_carriers_title')}
        meta={t('web.carriers_window', { hours: formatNumber(24, i18n.language) })}
      />
      <PanelBody className="space-y-5">
        {nodes.length > 0 && caps.supported === 0 ? (
          <CapabilityNotice
            state={caps.undetermined > 0 ? 'undetermined' : 'unsupported'}
            feature={t('web.feature_web')}
          />
        ) : carriersQuery.isLoading ? (
          <>
            <Skeleton className="h-2.5 w-full rounded-pill" />
            <Skeleton className="h-24 w-full" />
          </>
        ) : carriersQuery.isError || !stats ? (
          <ErrorState
            inset
            message={carriersQuery.error instanceof ApiError ? carriersQuery.error.message : t('common.error_generic')}
            retryLabel={t('common.refresh')}
            onRetry={() => void carriersQuery.refetch()}
          />
        ) : (
          <>
            <CarrierDistribution distribution={stats.carrier_selection_distribution} />
            <WebCounters stats={stats} />
          </>
        )}

        <p className="text-label text-mute" data-testid="fleet-web-capabilities">
          {t('web.fleet_capability_summary', { supported: caps.supported, total: caps.total })}
          {caps.undetermined > 0 && ` ${t('web.fleet_capability_undetermined', { count: caps.undetermined })}`}
        </p>
      </PanelBody>
    </Panel>
  );
}
