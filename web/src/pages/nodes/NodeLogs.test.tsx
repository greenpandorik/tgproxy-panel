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

  // Debt item 14: the pane used to render an "offline" placeholder and never
  // open the stream, so a node that had just reconnected stayed unusable until
  // the next node poll refreshed the flag.
  it('opens the stream even when the cached node status says offline', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online={false} />));
    await waitFor(() => expect(FakeEventSource.opened).toHaveLength(1));
    expect(FakeEventSource.opened[0]).toContain('/api/v1/nodes/11111111-1111-4111-8111-111111111111/logs');
    expect(screen.getByText('Connecting…')).toBeInTheDocument();
  });

  it('shows the offline message when the stream fails and the node is known offline', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online={false} />));
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.instances[0].onerror?.();
    await waitFor(() => expect(screen.getByText(/Node is offline/)).toBeInTheDocument());
    expect(FakeEventSource.instances[0].closed).toBe(true);
  });

  it('shows the connection-lost message when the stream fails on an online node', async () => {
    render(wrap(<NodeLogs nodeId="11111111-1111-4111-8111-111111111111" online />));
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.instances[0].onerror?.();
    await waitFor(() => expect(screen.getByText('Connection lost')).toBeInTheDocument());
  });
});
