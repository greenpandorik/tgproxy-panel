import type { CSSProperties } from 'react';

import { useBranding } from '@/api/branding';
import { useBrandingIdentity } from '@/theme/ThemeProvider';
import { DEFAULT_PANEL_NAME } from '@/components/brand/brand';
import { Logo } from '@/components/brand/Logo';
import { ENTER_CLASS } from '@/components/ui/motion';
import { cn } from '@/lib/utils';

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

// The operator's mark: their uploaded logo, or the brand mark and the panel name.
export function LoginWordmark({ className }: { className?: string }) {
  const { branding, theme } = useBrandingIdentity();
  const logoUrl = theme === 'dark' ? branding?.logo_dark_url || branding?.logo_url : branding?.logo_url;
  const name = branding?.panel_name || DEFAULT_PANEL_NAME;

  return (
    <div className={cn('flex items-center gap-2.5 text-title', className)}>
      {logoUrl ? (
        <img src={logoUrl} alt={name} className="max-h-7 max-w-[180px] object-contain" />
      ) : (
        <>
          <Logo size={24} />
          <span className="truncate">{name}</span>
        </>
      )}
    </div>
  );
}

/**
 * The same mark the operator set, at the size the column can afford. Nothing about the
 * deployment is said here: the login page is served to anyone who finds the address, and
 * the fleet's size, health and version are the operator's business, not a visitor's.
 */
function BrandMark() {
  const { branding, theme } = useBrandingIdentity();
  const logoUrl = theme === 'dark' ? branding?.logo_dark_url || branding?.logo_url : branding?.logo_url;
  const name = branding?.panel_name || DEFAULT_PANEL_NAME;

  return (
    <div data-testid="login-brand-mark" className={cn(ENTER_CLASS, 'flex flex-col items-center gap-6 text-center')}>
      {logoUrl ? (
        <img src={logoUrl} alt={name} className="max-h-24 max-w-[320px] object-contain" />
      ) : (
        <>
          <Logo size={72} />
          <span className="max-w-[420px] truncate text-heading text-foreground">{name}</span>
        </>
      )}
    </div>
  );
}

/** Wordmark and mark on the patterned ground: the whole left column. */
export function LoginStatusPanel() {
  const { data: branding } = useBranding();

  return (
    <aside
      data-testid="login-status-panel"
      className="relative flex flex-col overflow-hidden border-r border-hairline bg-background px-12 py-10"
    >
      {/* Operator-supplied backdrop, kept behind the pattern and dimmed to 22%
          so a photo can never take contrast away from the mark in front of it. */}
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
        <BrandMark />
      </div>
    </aside>
  );
}
