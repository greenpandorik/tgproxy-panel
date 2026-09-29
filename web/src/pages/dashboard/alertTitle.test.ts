import { beforeEach, expect, it } from 'vitest';

import i18n, { setLang } from '@/i18n';

import { alertTitle } from './alertTitle';

const title = (kind: string, message = 'raw server line') => alertTitle({ kind, message }, i18n.t.bind(i18n), i18n);

beforeEach(() => setLang('ru'));

it('names every incident the server raises in the interface language', () => {
  expect(title('node_offline')).toBe('Сервер не выходит на связь');
  expect(title('disk_pressure')).toBe('Заканчивается место на диске');
  expect(title('diagnostic_telemt_tls_front_errors', 'Scheduled check: telemt / tls_front_errors: …')).toBe('Диагностика: Сбои TLS-рукопожатий');
  expect(title('diagnostic_public_addresses_104.239.66.129')).toBe('Диагностика: адрес 104.239.66.129');
  expect(title('probe_isp-a_stale')).toBe('Внешняя проверка isp-a давно не присылала отчёт');
  expect(title('probe_isp_b_faketls')).toMatch(/^Внешняя проверка isp_b: не прошла/);
});

it('falls back to the server line for a kind it does not know', () => {
  expect(title('something_new', 'Something happened')).toBe('Something happened');
});
