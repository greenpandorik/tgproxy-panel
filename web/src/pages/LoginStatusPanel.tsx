import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';

import { useBranding } from '@/api/branding';
import { usePublicStatus } from '@/api/status';
import type { PublicStatus } from '@/api/status';
import { DEFAULT_PANEL_NAME } from '@/components/brand/brand';
import { Logo } from '@/components/brand/Logo';
import { ENTER_CLASS } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

/*
 * The left half of the login screen (variant A, see
 * docs/superpowers/specs/2026-09-05-ui-redesign.md).
 *
 * Everything on this panel is public by construction: it renders only what
 * `GET /api/v1/status/public` returns - a version, node counts and the relay
 * commit - and never a hostname or a node name, because it is shown to whoever
 * loads the page. The mockup's per-node rows and sparklines are deliberately
 * not reproduced: there is no honest data behind them before sign-in, and a
 * decorative fake chart on a control panel is worse than an empty column.
 *
 * The panel is the machine talking - monospace, dim, hairlines - so that the
 * single solid element on the screen is the sign-in button opposite it.
 */

/** `--line` grid, 48px, faded out by a radial mask so it never reaches an edge. */
const GRID_PATTERN: CSSProperties = {
  backgroundImage: 'linear-gradient(var(--line) 1px, transparent 1px), linear-gradient(90deg, var(--line) 1px, transparent 1px)',
  backgroundSize: '48px 48px',
  maskImage: 'radial-gradient(700px 500px at 45% 55%, #000 30%, transparent 100%)',
  WebkitMaskImage: 'radial-gradient(700px 500px at 45% 55%, #000 30%, transparent 100%)',
};

/** The one gradient the spec allows anywhere in the panel, capped at 10% alpha. */
const BRAND_GLOW: CSSProperties = {
  backgroundImage:
    'radial-gradient(1200px 600px at 20% -10%, color-mix(in oklab, var(--brand-primary) 10%, transparent), transparent 60%)',
};

/** ok while every node reports in, warn while some do, err once none do. */
function nodesTone(status: PublicStatus): string {
  if (status.nodes_total === 0) return 'bg-pending';
  if (status.nodes_online >= status.nodes_total) return 'bg-ok';
  return status.nodes_online > 0 ? 'bg-degraded' : 'bg-offline';
}

function Dot({ className }: { className: string }) {
  return <span aria-hidden="true" className={cn('size-[7px] shrink-0 rounded-pill', className)} />;
}

function Row({ label, value, dot }: { label: string; value: string; dot?: string }) {
  return (
    <div className="flex items-center gap-4 border-b border-hairline px-4 py-3 last:border-b-0">
      <dt className="flex min-w-0 items-center gap-2.5 text-mute">
        {dot ? <Dot className={dot} /> : <span aria-hidden="true" className="size-[7px] shrink-0" />}
        <span className="truncate">{label}</span>
      </dt>
      <dd className="tabular ml-auto shrink-0 text-foreground">{value}</dd>
    </div>
  );
}

/** Four bars in the shape of the four rows, using the panel's own skeleton. */
function SkeletonRows() {
  return (
    <div aria-hidden="true" data-testid="login-status-skeleton">
      {[36, 28, 32, 20].map((w, i) => (
        <div key={w} className="flex items-center gap-4 border-b border-hairline px-4 py-3 last:border-b-0">
          <Skeleton className="h-2.5" style={{ width: `${w * 2.6}px` }} />
          <Skeleton className="ml-auto h-2.5" style={{ width: `${44 + i * 8}px` }} />
        </div>
      ))}
    </div>
  );
}

function StatusCard() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = usePublicStatus();

  return (
    <section
      className={cn(
        ENTER_CLASS,
        'mono relative w-full max-w-[560px] rounded-surface border border-hairline-strong bg-surface/85 text-mono backdrop-blur-md',
      )}
    >
      <h2 className="border-b border-hairline px-4 py-3 text-title text-foreground">{t('login.status_title')}</h2>
      {isLoading && <SkeletonRows />}
      {!isLoading && (isError || !data) && <p className="px-4 py-3 text-mute">{t('login.status_unavailable')}</p>}
      {!isLoading && !isError && data && (
        <dl>
          <Row label={t('login.status_nodes')} value={`${data.nodes_online} / ${data.nodes_total}`} dot={nodesTone(data)} />
          <Row label={t('login.status_relay')} value={data.relay_commit || '—'} />
          <Row label={t('login.status_panel')} value={data.version ? `v${data.version}` : '—'} />
          {/* The answer arriving at all is what "api ok" means here. */}
          <Row label={t('login.status_api')} value={t('login.status_ok')} dot="bg-ok" />
        </dl>
      )}
    </section>
  );
}

/**
 * `v1.0.0  api ok  3 нод` - the same three facts as the panel footer, reused
 * under the form on narrow screens where there is no panel.
 */
export function LoginStatusLine({ className }: { className?: string }) {
  const { t } = useTranslation();
  const { data, isError } = usePublicStatus();

  if (isError || !data) {
    return isError ? <p className={cn('mono text-micro text-mute', className)}>{t('login.status_unavailable')}</p> : null;
  }

  return (
    <p className={cn('mono flex flex-wrap gap-x-6 gap-y-1 text-micro text-mute', className)}>
      <span>v{data.version}</span>
      <span>
        {t('login.status_api')} {t('login.status_ok')}
      </span>
      <span>{t('login.footer_nodes', { count: data.nodes_total })}</span>
    </p>
  );
}

/**
 * The operator's mark: their uploaded logo, or the brand mark and the panel name.
 * Top-left of the panel on a wide screen; above the heading on a phone, where
 * there is no panel and the form would otherwise be unbranded.
 */
export function LoginWordmark({ className }: { className?: string }) {
  const { data: branding } = useBranding();
  const name = branding?.panel_name || DEFAULT_PANEL_NAME;

  return (
    <div className={cn('flex items-center gap-2.5 text-title', className)}>
      {branding?.logo_url ? (
        <img src={branding.logo_url} alt={name} className="max-h-7 max-w-[180px] object-contain" />
      ) : (
        <>
          <Logo size={24} />
          <span className="truncate">{name}</span>
        </>
      )}
    </div>
  );
}

/** Wordmark, status card and footer on the patterned ground: the whole left column. */
export function LoginStatusPanel() {
  const { data: branding } = useBranding();

  return (
    <aside
      data-testid="login-status-panel"
      className="relative flex flex-col justify-between overflow-hidden border-r border-hairline bg-background px-12 py-10"
    >
      {/* Operator-supplied backdrop, kept behind the pattern and dimmed to 22%
          so a photo can never take contrast away from the card in front of it. */}
      {branding?.login_bg_url && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 bg-cover bg-center"
          style={{
            backgroundImage: `linear-gradient(0deg, color-mix(in oklab, var(--bg) 78%, transparent), color-mix(in oklab, var(--bg) 78%, transparent)), url(${branding.login_bg_url})`,
          }}
        />
      )}
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 opacity-50" style={GRID_PATTERN} />
      <div aria-hidden="true" className="pointer-events-none absolute inset-0" style={BRAND_GLOW} />

      <LoginWordmark className="relative" />

      <div className="relative flex flex-1 items-center justify-center py-10">
        <StatusCard />
      </div>

      <LoginStatusLine className="relative" />
    </aside>
  );
}
