import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import '@/i18n';
import { setLang } from '@/i18n';

import { NodeVersions, agentBehind } from './NodeVersions';

const node = (telemt: string, agent: string) => ({
  engine: 'telemt' as const,
  telemt_version: telemt,
  tproxy_version: '',
  agent_version: agent,
});

describe('agentBehind', () => {
  it('flags a minor release behind the panel, not a patch one', () => {
    expect(agentBehind('2.14.0', '2.15.2')).toBe(true);
    expect(agentBehind('1.9.9', '2.0.0')).toBe(true);
    expect(agentBehind('2.15.0', '2.15.2')).toBe(false);
    expect(agentBehind('2.16.0', '2.15.2')).toBe(false);
    expect(agentBehind('dev', '2.15.2')).toBe(false);
    expect(agentBehind('2.14.0', undefined)).toBe(false);
  });
});

describe('NodeVersions', () => {
  beforeEach(() => {
    void setLang('ru');
  });

  it('shows both versions and marks the ones with a newer release', () => {
    render(<NodeVersions node={node('3.5.7', '2.14.0')} pinnedTelemt="3.5.9" panelVersion="2.15.2" />);
    expect(screen.getByTitle('Есть telemt 3.5.9')).toHaveTextContent('telemt3.5.7');
    expect(screen.getByTitle('Есть агент 2.15.2')).toHaveTextContent('агент2.14.0');
  });

  it('leaves up-to-date versions unmarked', () => {
    render(<NodeVersions node={node('3.5.9', '2.15.0')} pinnedTelemt="3.5.9" panelVersion="2.15.2" />);
    expect(screen.getByText('3.5.9')).toBeInTheDocument();
    expect(screen.getByText('2.15.0')).toBeInTheDocument();
    expect(screen.queryByTitle(/^Есть/)).toBeNull();
  });
});
