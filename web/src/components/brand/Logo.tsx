import { cn } from '@/lib/utils';

/*
 * The TGProxy Panel mark: a rotated square outline with an axis-aligned
 * square inside - the wrapper around the payload, which is what a proxy
 * that hides MTProto inside WEB/Fake-TLS traffic does. Two shapes, so it
 * still reads at 16px; the outline follows `currentColor`, the core takes
 * the operator's brand hue so it agrees with the active-nav marker next to
 * it. The same geometry is in web/public/favicon.svg, web/public/logo.svg
 * and the subscription page template; keep them in step.
 */

/** The outline, on a 24-unit grid. Shared with the SVG files above. */
export const MARK_PATH = 'M12 1.75L22.25 12 12 22.25 1.75 12Z';

export type LogoSize = 16 | 24 | 40;

interface LogoProps {
  /** 16 in the sidebar rail, 24 on the login wordmark, 40 for standalone use. */
  size?: LogoSize;
  className?: string;
  /**
   * Accessible name. Omit when the mark sits next to the panel name (the
   * common case) - the SVG is then hidden from assistive tech as decoration.
   */
  title?: string;
  /** Fill of the core. Defaults to the runtime brand hue. */
  accent?: string;
}

export function Logo({ size = 16, className, title, accent = 'var(--brand-primary)' }: LogoProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className={cn('shrink-0', className)}
      role={title ? 'img' : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      data-testid="brand-logo"
    >
      <path d={MARK_PATH} stroke="currentColor" strokeWidth={2.4} strokeLinejoin="round" />
      <rect x={9} y={9} width={6} height={6} rx={1} fill={accent} />
    </svg>
  );
}
