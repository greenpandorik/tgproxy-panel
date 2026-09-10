import { cn } from '@/lib/utils';

/** The outline, on a 24-unit grid. Shared with the SVG files above. */
export const MARK_PATH = 'M12 1.75L22.25 12 12 22.25 1.75 12Z';

export type LogoSize = 16 | 24 | 40 | 72;

interface LogoProps {
  /** 16 in the sidebar rail, 24 on the login wordmark, 40 standalone, 72 on the login page. */
  size?: LogoSize;
  className?: string;
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
