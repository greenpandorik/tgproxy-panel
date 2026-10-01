import { describe, expect, it } from 'vitest';

import { parseImport, readSecret } from './importList';

const A = '0123456789abcdef0123456789abcdef';
const B = 'fedcba9876543210fedcba9876543210';
const domainHex = [...'cdn.example.com'].map((c) => c.charCodeAt(0).toString(16)).join('');

function base64url(hex: string): string {
  const bin = hex.match(/../g)!.map((h) => String.fromCharCode(parseInt(h, 16))).join('');
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

describe('readSecret', () => {
  it('reads plain, dd and Fake-TLS secrets in hex and base64', () => {
    expect(readSecret(A.toUpperCase())).toEqual({ secret: A });
    expect(readSecret(`dd${A}`)).toEqual({ secret: A });
    expect(readSecret(`ee${A}${domainHex}`)).toEqual({ secret: A, domain: 'cdn.example.com' });
    expect(readSecret(base64url(`ee${A}${domainHex}`))).toEqual({ secret: A, domain: 'cdn.example.com' });
    expect(readSecret('abc')).toBeNull();
    expect(readSecret(`ff${A}`)).toBeNull();
  });
});

describe('parseImport', () => {
  it('takes names, contacts and secrets from the usual shapes', () => {
    const parsed = parseImport(
      [
        '# exported from the old panel',
        '[access.users]',
        `ivan = "${A}"`,
        `Анна Петрова: ${B} : @anna`,
        '',
        `tg://proxy?server=old.example.com&port=443&secret=ee${'11'.repeat(16)}${domainHex}`,
        `oleg https://t.me/proxy?server=old.example.com&port=443&secret=dd${'22'.repeat(16)}`,
        `"maria"\t${'33'.repeat(16)}`,
      ].join('\n'),
    );
    expect(parsed.lines.map((l) => [l.line, l.label, l.owner_label, l.secret, l.auto_name])).toEqual([
      [3, 'ivan', '', A, false],
      [4, 'Анна Петрова', '@anna', B, false],
      [6, 'user-111111', '', '11'.repeat(16), true],
      [7, 'oleg', '', '22'.repeat(16), false],
      [8, 'maria', '', '33'.repeat(16), false],
    ]);
    expect(parsed.ready).toHaveLength(5);
    expect(parsed.domains).toEqual(['cdn.example.com']);
    expect(parsed.servers).toEqual(['old.example.com:443']);
  });

  it('reads t.me links written without a scheme and base64 secrets with a plus', () => {
    const plus = base64url(`ee${'fb'.repeat(16)}${domainHex}`).replace(/-/g, '+').replace(/_/g, '/');
    expect(plus).toContain('+');
    const parsed = parseImport(`alice t.me/proxy?server=1.2.3.4&port=443&secret=${A}\nbob tg://proxy?server=1.2.3.4&port=443&secret=${plus}`);
    expect(parsed.lines.map((l) => [l.label, l.secret, l.problem])).toEqual([
      ['alice', A, undefined],
      ['bob', 'fb'.repeat(16), undefined],
    ]);
    expect(parsed.servers).toEqual(['1.2.3.4:443']);
  });

  it('marks lines it cannot use and repeated secrets', () => {
    const parsed = parseImport([`a ${A}`, 'just a name', `b ${'9'.repeat(31)}`, `c tg://proxy?server=x&secret=zz`, `d ${A}`].join('\n'));
    expect(parsed.lines.map((l) => [l.line, l.problem, l.same_as])).toEqual([
      [1, undefined, undefined],
      [2, 'no_secret', undefined],
      [3, 'no_secret', undefined],
      [4, 'bad_secret', undefined],
      [5, 'duplicate', 1],
    ]);
    expect(parsed.ready.map((l) => l.label)).toEqual(['a']);
  });
});
