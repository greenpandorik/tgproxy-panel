import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';
import { useNode } from '@/api/nodes';
import { useAuth } from '@/auth/AuthProvider';
import { ApiError } from '@/lib/api';

import { NodeDetailPage } from './NodeDetailPage';

import type * as NodesApi from '@/api/nodes';

vi.mock('@/auth/AuthProvider', () => ({ useAuth: vi.fn() }));
vi.mock('@/api/nodes', async (importOriginal) => ({
  ...(await importOriginal<typeof NodesApi>()),
  useNode: vi.fn(),
}));

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/nodes/:id" element={<NodeDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('NodeDetailPage', () => {
  beforeEach(() => {
    setLang('en');
    vi.mocked(useAuth).mockReturnValue({ isWriter: true } as unknown as ReturnType<typeof useAuth>);
  });

  it('says the server is gone and leads back to the list when it does not exist', () => {
    vi.mocked(useNode).mockReturnValue({
      isLoading: false,
      isError: true,
      error: new ApiError(404, 'not_found', 'not found'),
      data: undefined,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof useNode>);

    renderAt('/nodes/00000000-0000-0000-0000-000000000000');

    expect(screen.getByText('Server not found')).toBeInTheDocument();
    expect(screen.queryByText('not found')).toBeNull();
    expect(screen.getByText('Back to servers').closest('a')).toHaveAttribute('href', '/nodes');
  });

  it('offers a retry when the server could not be loaded for another reason', () => {
    vi.mocked(useNode).mockReturnValue({
      isLoading: false,
      isError: true,
      error: new ApiError(500, 'internal', 'boom'),
      data: undefined,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof useNode>);

    renderAt('/nodes/00000000-0000-0000-0000-000000000000');

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument();
  });
});
