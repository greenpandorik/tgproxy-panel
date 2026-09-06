import { describe, expect, it } from 'vitest';

import en from '@/i18n/en.json';
import ru from '@/i18n/ru.json';

import { HELP_TOPIC_IDS, HELP_TOPICS, docsUrl, isHelpTopic } from './content';
import { HELP_ROUTES, topicForPath } from './routes';

// Every component source file as text, so the test can see which topics the
// HelpButtons name without a Node fs dependency in the browser tsconfig.
const SOURCES = import.meta.glob<string>('/src/**/*.tsx', { query: '?raw', import: 'default', eager: true });

/**
 * Every topic id named by a `<HelpButton topic=…>`: literal attributes as-is,
 * and, inside a `{…}` expression, the quoted strings that look like a topic id
 * (they contain a dot, or are a registered single-word id), so `mode === 'batch'`
 * in a ternary is not mistaken for one while `'keys.creat'` still is caught.
 */
function topicsUsedInSource(): { file: string; topic: string }[] {
  const found: { file: string; topic: string }[] = [];
  for (const [file, text] of Object.entries(SOURCES)) {
    if (file.endsWith('.test.tsx')) continue;
    for (const m of text.matchAll(/<HelpButton\b[^>]*\btopic=(?:"([^"]+)"|\{([^}]*)\})/g)) {
      if (m[1]) found.push({ file, topic: m[1] });
      else
        for (const s of m[2].matchAll(/'([\w.]+)'/g)) {
          if (s[1].includes('.') || isHelpTopic(s[1])) found.push({ file, topic: s[1] });
        }
    }
  }
  return found;
}

type Topic = { title: string; intro: string; fields: Record<string, Record<string, string>>; notes?: Record<string, string> };
const topicsOf = (bundle: unknown) => (bundle as { help: { topics: Record<string, Topic> } }).help.topics;

describe('help content', () => {
  it('every HelpButton in the source names a registered topic', () => {
    const used = topicsUsedInSource();
    expect(used.length).toBeGreaterThan(20);
    const unknown = used.filter((u) => !isHelpTopic(u.topic));
    expect(unknown).toEqual([]);
  });

  it('every route topic is registered and the app routes resolve to one', () => {
    for (const r of HELP_ROUTES) expect(isHelpTopic(r.topic)).toBe(true);
    expect(topicForPath('/')).toBe('dashboard');
    expect(topicForPath('/nodes')).toBe('nodes.list');
    expect(topicForPath('/nodes/1b6a')).toBe('nodes.detail');
    expect(topicForPath('/keys')).toBe('keys.list');
    expect(topicForPath('/sites')).toBe('sites.templates');
    expect(topicForPath('/sites/new')).toBe('sites.editor');
    expect(topicForPath('/sites/1b6a')).toBe('sites.editor');
    expect(topicForPath('/monitoring')).toBe('monitoring');
    expect(topicForPath('/audit')).toBe('audit');
    expect(topicForPath('/settings')).toBe('settings.branding');
    expect(topicForPath('/login')).toBeUndefined();
  });

  it.each([
    ['ru', ru],
    ['en', en],
  ])('%s.json has title, intro and name/what for every registered field, and no orphan text', (_lang, bundle) => {
    const topics = topicsOf(bundle);
    for (const id of HELP_TOPIC_IDS) {
      const topic = topics[id];
      expect(topic, `topic ${id}`).toBeDefined();
      expect(topic.title.trim(), `${id}.title`).not.toBe('');
      expect(topic.intro.trim(), `${id}.intro`).not.toBe('');
      for (const field of HELP_TOPICS[id].fields) {
        const f = topic.fields[field];
        expect(f, `${id}.fields.${field}`).toBeDefined();
        expect(f.name.trim(), `${id}.fields.${field}.name`).not.toBe('');
        expect(f.what.trim(), `${id}.fields.${field}.what`).not.toBe('');
        for (const k of Object.keys(f)) expect(['name', 'what', 'example', 'tip'], `${id}.fields.${field}.${k}`).toContain(k);
      }
      // Text for a field the registry does not list would never be rendered.
      expect(Object.keys(topic.fields).sort()).toEqual([...HELP_TOPICS[id].fields].sort());
      for (const note of Object.values(topic.notes ?? {})) expect(note.trim()).not.toBe('');
    }
    // And no topic text without a registry entry.
    expect(Object.keys(topics).sort()).toEqual([...HELP_TOPIC_IDS].sort());
  });

  it('links each topic to the setup guide in the reader’s language', () => {
    expect(docsUrl('nodes.create', 'ru')).toBe(
      'https://github.com/greenpandorik/tgproxy-panel/blob/main/docs/setup.ru.md#6-подключение-ноды',
    );
    expect(docsUrl('nodes.create', 'en-US')).toBe(
      'https://github.com/greenpandorik/tgproxy-panel/blob/main/docs/setup.en.md#6-adding-a-node',
    );
  });
});
