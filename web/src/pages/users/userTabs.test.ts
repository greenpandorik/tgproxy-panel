import { describe, expect, it } from 'vitest';

import { tabForErrors, tabOfField, tabsWithErrors } from './userTabs';

describe('user window tabs', () => {
  it('knows where each field lives, as the form or the server names it', () => {
    expect(tabOfField('label')).toBe('main');
    expect(tabOfField('expires_at')).toBe('main');
    expect(tabOfField('node_ids')).toBe('access');
    expect(tabOfField('limits.max_streams')).toBe('limits');
    expect(tabOfField('telemt_limits')).toBe('limits');
    expect(tabOfField('status')).toBeUndefined();
  });

  it('stays on the open tab when it has an error and goes to the first one otherwise', () => {
    const tabs = tabsWithErrors(['telemt_limits.quota_gb', 'node_ids', 'root']);
    expect([...tabs].sort()).toEqual(['access', 'limits']);
    expect(tabForErrors('limits', tabs)).toBe('limits');
    expect(tabForErrors('main', tabs)).toBe('access');
    expect(tabForErrors('stats', new Set())).toBe('stats');
  });
});
