// Thin fetch wrapper around the panel API. All mutating requests carry the
// double-submit CSRF token from the `tgwp_csrf` cookie (see internal/api/csrf.go);
// a 401 (other than on the login endpoint itself) broadcasts `tgwp:unauthorized`
// so AuthProvider can clear state and redirect to /login.

export class ApiError extends Error {
  status: number;
  code: string;
  fields: Record<string, string>;

  constructor(status: number, code: string, message: string, fields: Record<string, string> = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

interface ErrorEnvelope {
  error: { code: string; message: string; fields?: Record<string, string> };
}

function isErrorEnvelope(v: unknown): v is ErrorEnvelope {
  return typeof v === 'object' && v !== null && 'error' in v;
}

function csrf(): string {
  const m = document.cookie.match(/(?:^|; )tgwp_csrf=([^;]*)/);
  return m ? decodeURIComponent(m[1]) : '';
}

export const UNAUTHORIZED_EVENT = 'tgwp:unauthorized';

export async function request<T>(method: string, path: string, body?: unknown, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string> | undefined) };
  if (body !== undefined && !(body instanceof FormData)) headers['Content-Type'] = 'application/json';
  if (method !== 'GET') headers['X-CSRF-Token'] = csrf();

  const res = await fetch(path, {
    method,
    headers,
    body: body instanceof FormData ? body : body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
    ...init,
  });

  if (res.status === 401 && !path.endsWith('/auth/login')) {
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
  }

  if (res.status === 204) return undefined as T;

  const ct = res.headers.get('content-type') ?? '';
  const data: unknown = ct.includes('application/json') ? await res.json() : await res.text();

  if (!res.ok) {
    const e = isErrorEnvelope(data) ? data.error : { code: 'http_' + res.status, message: String(data) };
    throw new ApiError(res.status, e.code, e.message, e.fields ?? {});
  }
  return data as T;
}

export const api = {
  get: <T>(p: string, init?: RequestInit) => request<T>('GET', p, undefined, init),
  post: <T>(p: string, b?: unknown, init?: RequestInit) => request<T>('POST', p, b, init),
  put: <T>(p: string, b?: unknown, init?: RequestInit) => request<T>('PUT', p, b, init),
  patch: <T>(p: string, b?: unknown, init?: RequestInit) => request<T>('PATCH', p, b, init),
  del: <T>(p: string, init?: RequestInit) => request<T>('DELETE', p, undefined, init),
};
