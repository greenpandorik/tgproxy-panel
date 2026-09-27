import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom';
import { expect, it } from 'vitest';
import { SectionNav, SectionTabs, useSection } from './SectionNav';
function Fixture() {
  const [section, setSection] = useSection(['monitor', 'settings'], 'monitor');
  const location = useLocation();
  const nav = useNavigate();
  return (
    <>
      <SectionNav
        label="Sections"
        items={[
          { value: 'monitor', label: 'Monitor' },
          { value: 'settings', label: 'Settings' },
        ]}
        value={section}
        onChange={setSection}
      />
      <output>{location.search}</output>
      <button onClick={() => nav(-1)}>Back</button>
    </>
  );
}
it('preserves filters, supports keyboard and restores the previous section', async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={['/?filter=online']}>
      <Fixture />
    </MemoryRouter>,
  );
  expect(screen.getByRole('button', { name: 'Monitor' })).toHaveAttribute('aria-current', 'page');
  screen.getByRole('button', { name: 'Settings' }).focus();
  await user.keyboard('{Enter}');
  expect(screen.getByRole('button', { name: 'Settings' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('status')).toHaveTextContent('filter=online&section=settings');
  await user.click(screen.getByRole('button', { name: 'Back' }));
  expect(screen.getByRole('button', { name: 'Monitor' })).toHaveAttribute('aria-current', 'page');
});
it('uses the safe default for an unknown section', () => {
  render(
    <MemoryRouter initialEntries={['/?section=invalid']}>
      <Fixture />
    </MemoryRouter>,
  );
  expect(screen.getByRole('button', { name: 'Monitor' })).toHaveAttribute('aria-current', 'page');
});

it('carries the groups into the narrow-layout select, without a heading for a group of one', () => {
  render(
    <MemoryRouter>
      <SectionNav
        label="Sections"
        items={[
          { value: 'overview', label: 'Health', group: 'Monitor' },
          { value: 'logs', label: 'Logs', group: 'Monitor' },
          { value: 'settings', label: 'Maintenance', group: 'Maintain' },
        ]}
        value="overview"
        onChange={() => {}}
      />
    </MemoryRouter>,
  );

  const select = screen.getByRole('combobox');
  expect(Array.from(select.querySelectorAll('optgroup')).map((g) => g.label)).toEqual(['Monitor']);
  expect(Array.from(select.querySelectorAll('option')).map((o) => o.textContent)).toEqual(['Health', 'Logs', 'Maintenance']);
});

it('draws the tabs in order with a rule between groups and marks the current one', async () => {
  const user = userEvent.setup();
  const seen: string[] = [];
  render(
    <MemoryRouter>
      <SectionTabs
        label="Sections"
        items={[
          { value: 'overview', label: 'Health', group: 'Monitor' },
          { value: 'logs', label: 'Logs', group: 'Monitor' },
          { value: 'proxy', label: 'Proxy', group: 'Configure' },
          { value: 'settings', label: 'Maintenance', group: 'Maintain' },
        ]}
        value="logs"
        onChange={(v) => seen.push(v)}
      />
    </MemoryRouter>,
  );

  const tabs = screen.getAllByRole('button');
  expect(tabs.map((b) => b.textContent)).toEqual(['Health', 'Logs', 'Proxy', 'Maintenance']);
  expect(screen.getByRole('button', { name: 'Logs' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('button', { name: 'Health' })).not.toHaveAttribute('aria-current');
  expect(screen.getAllByTestId('section-tabs-divider')).toHaveLength(2);

  await user.click(screen.getByRole('button', { name: 'Proxy' }));
  expect(seen).toEqual(['proxy']);
});
