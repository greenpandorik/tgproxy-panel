import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { api, ApiError, UNAUTHORIZED_EVENT, request } from './api';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

describe('api client', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
    document.cookie = 'tgwp_csrf=; Max-Age=0; path=/';
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('maps a 422 validation envelope to an ApiError with fields', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse(422, {
        error: { code: 'validation', message: 'invalid input', fields: { hostname: 'required' } },
      }),
    );

    await expect(request('POST', '/api/v1/nodes', { name: 'x' })).rejects.toMatchObject({
      status: 422,
      code: 'validation',
      message: 'invalid input',
      fields: { hostname: 'required' },
    });
  });

  it('rejects with an ApiError instance', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse(401, { error: { code: 'invalid_credentials', message: 'wrong username or password' } }),
    );
    try {
      await api.post('/api/v1/auth/login', { username: 'x', password: 'y' });
      expect.unreachable('expected rejection');
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
    }
  });

  it('defaults fields to an empty object when the envelope omits them', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse(500, { error: { code: 'internal', message: 'internal error' } }));
    await expect(request('GET', '/api/v1/nodes')).rejects.toMatchObject({ fields: {} });
  });

  it('falls back to an http_<status> code for a non-JSON error body', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('boom', { status: 502, headers: { 'content-type': 'text/plain' } }));
    await expect(request('GET', '/api/v1/nodes')).rejects.toMatchObject({ status: 502, code: 'http_502' });
  });

  it('returns undefined for a 204 response without reading the body', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(request('DELETE', '/api/v1/nodes/1')).resolves.toBeUndefined();
  });

  it('sends the CSRF cookie value as X-CSRF-Token on mutating requests only', async () => {
    document.cookie = 'tgwp_csrf=secret-token; path=/';
    vi.mocked(fetch).mockImplementation(() => Promise.resolve(jsonResponse(200, { ok: true })));

    await request('GET', '/api/v1/nodes');
    const getHeaders = vi.mocked(fetch).mock.calls[0][1]?.headers as Record<string, string>;
    expect(getHeaders['X-CSRF-Token']).toBeUndefined();

    await request('POST', '/api/v1/nodes', { name: 'x' });
    const postHeaders = vi.mocked(fetch).mock.calls[1][1]?.headers as Record<string, string>;
    expect(postHeaders['X-CSRF-Token']).toBe('secret-token');
  });

  it('dispatches tgwp:unauthorized on a 401 for a non-login request', async () => {
    const handler = vi.fn();
    window.addEventListener(UNAUTHORIZED_EVENT, handler);
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse(401, { error: { code: 'unauthorized', message: 'authentication required' } }),
    );

    await expect(request('GET', '/api/v1/auth/me')).rejects.toBeInstanceOf(ApiError);
    expect(handler).toHaveBeenCalledOnce();
    window.removeEventListener(UNAUTHORIZED_EVENT, handler);
  });

  it('does not dispatch tgwp:unauthorized for a failed login itself', async () => {
    const handler = vi.fn();
    window.addEventListener(UNAUTHORIZED_EVENT, handler);
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse(401, { error: { code: 'invalid_credentials', message: 'wrong username or password' } }),
    );

    await expect(request('POST', '/api/v1/auth/login', { username: 'a', password: 'b' })).rejects.toBeInstanceOf(ApiError);
    expect(handler).not.toHaveBeenCalled();
    window.removeEventListener(UNAUTHORIZED_EVENT, handler);
  });
});
