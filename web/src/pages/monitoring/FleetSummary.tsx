import { useTranslation } from 'react-i18next';

import { isMetricPresent } from '@/components/common/metric';
import { StatStrip } from '@/components/common/StatStrip';
import { formatBytes, formatNumber } from '@/lib/format';
import { dcTone } from '@/pages/nodes/dcDisplay';

import { CERT_WARN_DAYS, formatDays } from './certDisplay';
import { currentThroughput, nearestCertificate, worstRoute } from './fleetFacts';

import type { MonitoringOverview, Node } from '@/api/types';
import type { StatStripItem } from '@/components/common/StatStrip';

/** Five numbers about the whole fleet, each from data the panel actually holds. */
export function FleetSummary({ nodes, overview, loading }: { nodes: Node[]; overview?: MonitoringOverview; loading?: boolean }) {
  const { t, i18n } = useTranslation();
  const num = (v: number) => formatNumber(v, i18n.language);
  const none = t('common.not_available');

  const online = nodes.filter((n) => n.status === 'online' || n.status === 'degraded').length;
  const down = nodes.filter((n) => n.status === 'offline').map((n) => n.name);
  const people = overview?.fleet?.people_online;
  const connections = overview?.fleet?.connections;
  const route = worstRoute(nodes);
  const routeTone = route ? dcTone(route.ms) : 'neutral';
  const cert = nearestCertificate(overview?.nodes ?? []);
  const throughput = currentThroughput(overview?.series ?? {});

  const items: StatStripItem[] = [
    {
      id: 'online',
      label: t('monitoring.fleet_online'),
      value: `${num(online)} / ${num(nodes.length)}`,
      tone: down.length === 0 ? undefined : online === 0 ? 'err' : 'warn',
      sub:
        down.length === 0
          ? undefined
          : down.length === 1
            ? t('dashboard.tile_nodes_down_one', { name: down[0] })
            : t('dashboard.tile_nodes_down_many', { name: down[0], more: num(down.length - 1) }),
      to: '/nodes',
    },
    {
      id: 'people',
      label: t('common.people_online'),
      value: isMetricPresent(people) ? t('common.approx', { value: num(people) }) : none,
      sub: isMetricPresent(connections) ? t('common.connections_count', { value: num(connections) }) : undefined,
    },
    {
      id: 'route',
      label: t('monitoring.worst_route'),
      value: route ? `${num(Math.round(route.ms))} ${t('common.ms')}` : none,
      tone: routeTone === 'warn' || routeTone === 'err' ? routeTone : undefined,
      sub: route ? t('monitoring.worst_route_where', { node: route.node, dc: route.dc }) : undefined,
    },
    {
      id: 'certs',
      label: t('monitoring.certs'),
      value: !cert
        ? none
        : cert.days < 0
          ? t('monitoring.cert_expired')
          : cert.days < CERT_WARN_DAYS
            ? t('monitoring.cert_expires_in', { days: formatDays(cert.days, i18n.language) })
            : t('monitoring.certs_ok'),
      tone: !cert ? undefined : cert.days < 0 ? 'err' : cert.days < CERT_WARN_DAYS ? 'warn' : undefined,
      sub: !cert
        ? undefined
        : cert.days < CERT_WARN_DAYS
          ? cert.node
          : t('monitoring.certs_nearest', { days: formatDays(cert.days, i18n.language) }),
    },
    {
      id: 'traffic',
      label: t('monitoring.fleet_traffic'),
      value: throughput === null ? none : t('common.per_second', { value: formatBytes(throughput, 1, i18n.language) }),
    },
  ];

  return <StatStrip items={items} loading={loading} label={t('monitoring.fleet_title')} />;
}
