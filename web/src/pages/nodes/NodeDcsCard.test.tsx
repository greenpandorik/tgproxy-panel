import { fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { NodeDcsCard } from './NodeDcsCard';

import type { NodeHealth } from '@/api/types';

function health(extra: Partial<NodeHealth> = {}): NodeHealth {
  return {
    relay_active: true,
    mtproxy_active: true,
    caddy_active: true,
    healthz: true,
    readyz: true,
    tproxy_version: 'telemt 3.5.7',
    agent_version: '1',
    uptime_seconds: 10,
    cpu_percent: 1,
    mem_used_percent: 1,
    disk_used_percent: 1,
    profile_count: 0,
    ...extra,
  };
}

const reporting = health({
  dc_data_available: true,
  upstream_healthy: true,
  upstream_fails: 2,
  effective_latency_ms: 120,
  connect_success_total: 58,
  connect_fail_total: 2,
  upstream_last_check_age_secs: 29,
  dcs: [
    { dc: 2, latency_ms: 36.5, known: true, ip_preference: 'ipv4' },
    { dc: 1, latency_ms: 197.9, known: true, ip_preference: 'ipv6' },
    { dc: 4, latency_ms: 812, known: true, ip_preference: 'ipv4' },
    { dc: 5, latency_ms: 0, known: false, ip_preference: '' },
  ],
});

describe('NodeDcsCard', () => {
  beforeEach(() => {
    setLang('en');
  });

  it('prints the route line in its tone and one toned row per DC, in DC order', () => {
    render(<NodeDcsCard engine="telemt" offline={false} health={reporting} error={false} onRetry={() => {}} />);

    const route = screen.getByTestId('dc-route');
    expect(route).toHaveAttribute('data-tone', 'warn');
    expect(route).toHaveTextContent('direct route');
    expect(route).toHaveTextContent('connections 58 of 60');
    expect(route).toHaveTextContent(/checked .*29.* ago/);
    expect(route.querySelector('.bg-warn')).not.toBeNull();

    const rows = screen.getAllByRole('row').slice(1);
    expect(rows.map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['DC1', 'DC2', 'DC4', 'DC5']);

    const figures = screen.getAllByTestId('dc-latency');
    expect(figures.map((f) => f.getAttribute('data-tone'))).toEqual(['warn', 'ok', 'err', 'neutral']);
    expect(figures[0]).toHaveTextContent('198 ms');
    expect(figures[0].className).toContain('text-warn');
    expect(figures[1]).toHaveTextContent('37 ms');
    expect(figures[1].className).toContain('text-mute');
    expect(figures[2]).toHaveTextContent('812 ms');
    expect(figures[2].className).toContain('text-err');
    // An unmeasured DC is a dash in --dim, never a coloured zero.
    expect(figures[3]).toHaveTextContent('—');
    expect(figures[3].className).toContain('text-dim');

    expect(within(rows[0]).getAllByRole('cell')[2]).toHaveTextContent('ipv6');
    expect(within(rows[3]).getAllByRole('cell')[2]).toHaveTextContent('—');
  });

  it('paints the route red when telemt says it is down, and green when it has no fails', () => {
    const { rerender } = render(
      <NodeDcsCard
        engine="telemt"
        offline={false}
        health={{ ...reporting, upstream_healthy: false }}
        error={false}
        onRetry={() => {}}
      />,
    );
    expect(screen.getByTestId('dc-route')).toHaveAttribute('data-tone', 'err');

    rerender(
      <NodeDcsCard
        engine="telemt"
        offline={false}
        health={{ ...reporting, upstream_fails: 0 }}
        error={false}
        onRetry={() => {}}
      />,
    );
    expect(screen.getByTestId('dc-route')).toHaveAttribute('data-tone', 'ok');
  });

  it('says so on a tproxy node instead of drawing an empty table', () => {
    render(<NodeDcsCard engine="tproxy" offline={false} health={health()} error={false} onRetry={() => {}} />);
    expect(screen.getByText('Not available on this engine')).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('distinguishes telemt not reporting from telemt reporting nothing yet', () => {
    const { rerender } = render(
      <NodeDcsCard
        engine="telemt"
        offline={false}
        health={health({ dc_data_available: false })}
        error={false}
        onRetry={() => {}}
      />,
    );
    expect(screen.getByText('telemt is not reporting datacenter data')).toBeInTheDocument();

    rerender(
      <NodeDcsCard
        engine="telemt"
        offline={false}
        health={health({ dc_data_available: true, dcs: [] })}
        error={false}
        onRetry={() => {}}
      />,
    );
    expect(screen.getByText('No datacenter data yet')).toBeInTheDocument();
  });

  it('shows the offline message, the loading shape and the error with a retry', () => {
    const onRetry = vi.fn();
    const { rerender } = render(<NodeDcsCard engine="telemt" offline health={undefined} error={false} onRetry={onRetry} />);
    expect(screen.getByText(/offline/i)).toBeInTheDocument();

    rerender(<NodeDcsCard engine="telemt" offline={false} health={undefined} error={false} onRetry={onRetry} />);
    expect(screen.queryByRole('table')).toBeNull();
    expect(screen.queryByText('DC1')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();

    rerender(<NodeDcsCard engine="telemt" offline={false} health={undefined} error onRetry={onRetry} />);
    fireEvent.click(within(screen.getByRole('alert')).getByRole('button'));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
