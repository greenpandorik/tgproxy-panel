export const MAX_IMPORT = 500;

export type ImportProblem = 'no_secret' | 'bad_secret' | 'duplicate';

export interface ImportLine {
  line: number;
  label: string;
  owner_label: string;
  secret: string;
  auto_name: boolean;
  problem?: ImportProblem;
  same_as?: number;
}

export interface ParsedImport {
  lines: ImportLine[];
  ready: ImportLine[];
  domains: string[];
  servers: string[];
}

const SKIP_RE = /^(#|\/\/|\[)/;
const TOML_RE = /^"?([^"=]+?)"?\s*=\s*"([^"]*)"\s*,?$/;
const URL_RE = /(?:tg:\/\/|https?:\/\/|(?<![\w./])t\.me\/)[^\s"'<>]+/i;
const HEX_RE = /(?<![0-9a-z])[0-9a-f]{32,}(?![0-9a-z])/i;
const EDGE_RE = /^[\s:;,=|"'-]+|[\s:;,=|"'-]+$/g;
const DOMAIN_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/i;

interface Secret {
  secret: string;
  domain?: string;
}

function fromBase64(raw: string): string | null {
  if (!/^[A-Za-z0-9+/_-]+=*$/.test(raw) || raw.length < 22) return null;
  try {
    const b64 = raw.replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '');
    const bin = atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4));
    return [...bin].map((c) => c.charCodeAt(0).toString(16).padStart(2, '0')).join('');
  } catch {
    return null;
  }
}

function asciiOf(hex: string): string {
  let out = '';
  for (let i = 0; i + 1 < hex.length; i += 2) out += String.fromCharCode(parseInt(hex.slice(i, i + 2), 16));
  return out;
}

/** Reads a proxy secret as written in a link or a config: plain, dd-prefixed or Fake-TLS, in hex or base64. */
export function readSecret(raw: string): Secret | null {
  const value = raw.trim();
  const hex = (/^[0-9a-f]+$/i.test(value) ? value : fromBase64(value))?.toLowerCase();
  if (!hex) return null;
  if (hex.length === 32) return { secret: hex };
  if (hex.length === 34 && hex.startsWith('dd')) return { secret: hex.slice(2) };
  if (hex.length >= 34 && hex.startsWith('ee')) {
    const domain = asciiOf(hex.slice(34)).toLowerCase();
    return { secret: hex.slice(2, 34), domain: DOMAIN_RE.test(domain) ? domain : undefined };
  }
  return null;
}

function clean(s: string): string {
  return s.replace(EDGE_RE, '').trim();
}

interface Found {
  label: string;
  owner_label: string;
  raw: string | null;
  server?: string;
}

function split(text: string): Found {
  const toml = TOML_RE.exec(text);
  if (toml && !toml[2].includes('://')) return { label: clean(toml[1]), owner_label: '', raw: toml[2] };

  const url = URL_RE.exec(text);
  if (url) {
    const query = url[0].includes('?') ? url[0].slice(url[0].indexOf('?') + 1).split('#')[0] : '';
    const params = new URLSearchParams(query);
    const host = params.get('server');
    const port = params.get('port');
    return {
      label: clean(text.slice(0, url.index)),
      owner_label: clean(text.slice(url.index + url[0].length)),
      raw: params.get('secret')?.replace(/ /g, '+') ?? null,
      server: host ? (port ? `${host}:${port}` : host) : undefined,
    };
  }

  const hex = HEX_RE.exec(text);
  if (hex) {
    return {
      label: clean(text.slice(0, hex.index)),
      owner_label: clean(text.slice(hex.index + hex[0].length)),
      raw: hex[0],
    };
  }
  return { label: clean(text), owner_label: '', raw: null };
}

/** Turns a pasted list into users: one per line, as name and secret, a telemt config line or a proxy link. */
export function parseImport(text: string): ParsedImport {
  const lines: ImportLine[] = [];
  const domains = new Set<string>();
  const servers = new Set<string>();
  const firstLine = new Map<string, number>();

  text.split(/\r?\n/).forEach((source, i) => {
    const trimmed = source.trim();
    if (!trimmed || SKIP_RE.test(trimmed)) return;
    const found = split(trimmed);
    const line: ImportLine = { line: i + 1, label: found.label, owner_label: found.owner_label, secret: '', auto_name: false };
    if (found.server) servers.add(found.server);
    const secret = found.raw === null ? null : readSecret(found.raw);
    if (found.raw === null) {
      line.problem = 'no_secret';
    } else if (!secret) {
      line.problem = 'bad_secret';
    } else {
      line.secret = secret.secret;
      if (secret.domain) domains.add(secret.domain);
      if (!line.label) {
        line.label = `user-${secret.secret.slice(0, 6)}`;
        line.auto_name = true;
      }
      const first = firstLine.get(secret.secret);
      if (first !== undefined) {
        line.problem = 'duplicate';
        line.same_as = first;
      } else {
        firstLine.set(secret.secret, line.line);
      }
    }
    lines.push(line);
  });

  return {
    lines,
    ready: lines.filter((l) => !l.problem),
    domains: [...domains],
    servers: [...servers],
  };
}
