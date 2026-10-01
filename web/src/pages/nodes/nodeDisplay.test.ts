import { describe, expect, it } from 'vitest';

import { peopleOrConnections } from './nodeDisplay';

describe('peopleOrConnections', () => {
  it('charts people, and connections only where history predates the count', () => {
    expect(peopleOrConnections({ people_online: 3, sessions_live: 17 })).toBe(3);
    expect(peopleOrConnections({ people_online: 0, sessions_live: 17 })).toBe(0);
    expect(peopleOrConnections({ people_online: null, sessions_live: 17 })).toBe(17);
    expect(peopleOrConnections({ sessions_live: 17 })).toBe(17);
  });
});
