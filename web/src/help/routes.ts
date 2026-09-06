import { matchPath } from 'react-router-dom';

import type { HelpTopic } from './content';

/**
 * Which help topic the `?` shortcut and the palette's "Help for this page"
 * open on a given route. Patterns are react-router's; the first match wins, so
 * the more specific `/sites/new` sits above `/sites/:id`.
 *
 * Settings is one route with tabs; `?` there opens the default tab's topic.
 * The header button on that page follows the selected tab instead.
 */
export const HELP_ROUTES: readonly { pattern: string; topic: HelpTopic }[] = [
  { pattern: '/', topic: 'dashboard' },
  { pattern: '/nodes', topic: 'nodes.list' },
  { pattern: '/nodes/:id', topic: 'nodes.detail' },
  { pattern: '/keys', topic: 'keys.list' },
  { pattern: '/sites', topic: 'sites.templates' },
  { pattern: '/sites/new', topic: 'sites.editor' },
  { pattern: '/sites/:id', topic: 'sites.editor' },
  { pattern: '/monitoring', topic: 'monitoring' },
  { pattern: '/audit', topic: 'audit' },
  { pattern: '/settings', topic: 'settings.branding' },
];

/** The topic for a pathname, or undefined for a route with no help (login, 404). */
export function topicForPath(pathname: string): HelpTopic | undefined {
  return HELP_ROUTES.find((r) => matchPath({ path: r.pattern, end: true }, pathname))?.topic;
}
