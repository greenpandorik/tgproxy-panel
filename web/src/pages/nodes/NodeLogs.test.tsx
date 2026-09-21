import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { NodeLogs } from './NodeLogs';

/** Minimal EventSource stand-in: records the URLs opened and exposes the handlers. */
class FakeEventSource {
  static opened: string[] = [];
  static instances: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  private listeners: Record<string, ((ev: unknown) => void)[]> = {};

  url: string;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.opened.push(url);
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, fn: (ev: unknown) => void): void {
    (this.listeners[type] ??= []).push(fn);
  }

  close(): void {
    this.closed = true;
  }

  /** Delivers one server-sent event to whatever the component registered for it. */
  emit(type: string, data: unknown): void {
    for (const fn of this.listeners[type] ?? []) {
      fn({ data: JSON.stringify(data) });
    }
  }
}

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

describe('NodeLogs', () => {
  beforeEach(() => {
    FakeEventSource.opened = [];
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 200 })));
    setLang('en');
  });

  it('opens the stream even when the cached node status says offline', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online={false} engine="tproxy" />));
    await waitFor(() => expect(FakeEventSource.opened).toHaveLength(1));
    expect(FakeEventSource.opened[0]).toContain('/api/v1/nodes/11111111-1111-4111-8111-111111111111/logs');
    expect(screen.getByText('Connecting…')).toBeInTheDocument();
  });

  it('shows the offline message when the stream fails and the node is known offline', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online={false} engine="tproxy" />));
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.instances[0].onerror?.();
    await waitFor(() => expect(screen.getByText(/Node is offline/)).toBeInTheDocument());
    expect(FakeEventSource.instances[0].closed).toBe(true);
  });

  it('shows the connection-lost message when the stream fails on an online node', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online engine="tproxy" />));
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.instances[0].onerror?.();
    await waitFor(() => expect(screen.getByText('Connection lost')).toBeInTheDocument());
  });
});

describe('NodeLogs per engine', () => {
  beforeEach(() => {
    FakeEventSource.opened = [];
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 200 })));
    setLang('en');
  });

  // A telemt node has no tproxy-server unit, so asking for its logs returns nothing at all and
  // the operator is left staring at an empty pane with no hint that they picked the wrong process.
  it('asks a telemt node for telemt, and offers the units it actually runs', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online engine="telemt" />));
    await waitFor(() => expect(FakeEventSource.opened).toHaveLength(1));
    expect(FakeEventSource.opened[0]).toContain('services=telemt');
    expect(FakeEventSource.opened[0]).not.toContain('tproxy-server');
    expect(screen.getByRole('switch', { name: 'telemt' })).toBeInTheDocument();
    expect(screen.queryByRole('switch', { name: /tproxy/i })).toBeNull();
  });

  it('asks a tproxy node for tproxy-server', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online engine="tproxy" />));
    await waitFor(() => expect(FakeEventSource.opened).toHaveLength(1));
    expect(FakeEventSource.opened[0]).toContain('services=tproxy-server');
  });

  // The agent's own journal is where an apply that never arrived shows up, on either engine.
  it('offers the agent journal on both engines', async () => {
    const { unmount } = render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online engine="telemt" />));
    expect(screen.getByRole('switch', { name: 'Agent' })).toBeInTheDocument();
    unmount();
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online engine="tproxy" />));
    expect(screen.getByRole('switch', { name: 'Agent' })).toBeInTheDocument();
  });

  // A stream that drops after delivering lines used to say nothing: the tail just stopped moving.
  it('says the connection was lost even when lines are already on screen', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online engine="telemt" />));
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const es = FakeEventSource.instances[0];
    es.emit('log', { service: 'telemt', line: 'started', unix_ms: 1 });
    await waitFor(() => expect(screen.getByText(/started/)).toBeInTheDocument());

    es.onerror?.();
    await waitFor(() => expect(screen.getByText('Connection lost')).toBeInTheDocument());
    expect(screen.getByText(/started/)).toBeInTheDocument(); // the lines already read stay
  });
});
