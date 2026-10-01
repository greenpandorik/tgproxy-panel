import { useTranslation } from 'react-i18next';

import { useReliability } from '@/api/reliability';
import { CollapsibleSection } from '@/components/common/CollapsibleSection';
import { Panel } from '@/components/common/Panel';
import { CAP_CARRIER_NEGOTIATION, nodeCapability } from '@/components/web/capability';
import { useWebPolicy } from '@/components/web/webPolicy';
import { WebPolicyCard } from '@/components/web/WebPolicyCard';
import { ENTER_CLASS } from '@/components/ui/motion';
import { cn } from '@/lib/utils';

import { NodeListenersCard } from './NodeListenersCard';
import { NodeReliability } from './NodeReliability';

import type { Summary } from './sectionSummaries';
import type { Node } from '@/api/types';

/** A telemt server's own settings: its addresses first, the transport and recovery rules behind a click. */
export function NodeProxyTab({ node, canEdit }: { node: Node; canEdit: boolean }) {
  const { t } = useTranslation();
  const capability = nodeCapability(node, CAP_CARRIER_NEGOTIATION);
  const policyQuery = useWebPolicy(node.id);
  const reliabilityQuery = useReliability(node.id, node.online);

  const policy: Summary = policyQuery.data
    ? {
        text:
          capability === 'unsupported'
            ? t('nodes.detail_policy_locked')
            : t(`web.preset_${policyQuery.data.policy.preset}`, policyQuery.data.policy.preset),
        tone: 'neutral',
      }
    : { text: policyQuery.isError ? t('nodes.detail_summary_error') : t('common.state.loading'), tone: 'neutral' };

  const state = reliabilityQuery.data;
  const primary = state && (state.policy.egress === 'socks5' ? state.policy.socks_address : state.policy.egress);
  const reliability: Summary = !node.online
    ? { text: t('nodes.detail_summary_offline'), tone: 'neutral' }
    : !state
      ? { text: reliabilityQuery.isError ? t('nodes.detail_summary_error') : t('common.state.loading'), tone: 'neutral' }
      : state.policy.maintenance
        ? { text: t('nodes.detail_reliability_maintenance'), tone: 'warn' }
        : state.policy.egress !== 'unmanaged' && state.active !== primary
          ? { text: t('nodes.detail_reliability_reserve'), tone: 'warn' }
          : {
              text: `${t(`reliability.${state.policy.recovery}`)} · ${t(`reliability.${state.policy.egress}`)}`,
              tone: 'neutral',
            };

  return (
    <div className={cn(ENTER_CLASS, 'flex flex-col gap-4')}>
      <NodeListenersCard node={node} canEdit={canEdit} />
      <Panel>
        <CollapsibleSection title={t('web.policy_title')} summary={policy.text} tone={policy.tone} keepMounted>
          <WebPolicyCard nodeId={node.id} capability={capability} />
        </CollapsibleSection>
        <CollapsibleSection title={t('reliability.title')} summary={reliability.text} tone={reliability.tone} keepMounted>
          <NodeReliability node={node} />
        </CollapsibleSection>
      </Panel>
    </div>
  );
}
