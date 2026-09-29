import { cn } from '@/lib/utils';

/** The mask outline, on a 24-unit grid. Shared with the SVG files in public/ and docs/brand/. */
export const MARK_PATH = 'M2.6 9.7c0-1.9 1.4-2.9 3.2-2.9h12.4c1.8 0 3.2 1 3.2 2.9 0 4.2-2.4 7.6-5.5 7.6-1.9 0-3-1.3-3.9-2.8-.9 1.5-2 2.8-3.9 2.8-3.1 0-5.5-3.4-5.5-7.6z';

/** The two eye holes, filled with the brand hue. */
export const MARK_EYES = [
  { cx: 8.1, cy: 11.5 },
  { cx: 15.9, cy: 11.5 },
] as const;

export type LogoSize = 16 | 24 | 40 | 72;

interface LogoProps {
  /** 16 in the sidebar rail, 24 on the login wordmark, 40 standalone, 72 on the login page. */
  size?: LogoSize;
  className?: string;
  title?: string;
  /** Fill of the eyes. Defaults to the runtime brand hue. */
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
      <path d={MARK_PATH} stroke="currentColor" strokeWidth={2} strokeLinejoin="round" />
      {MARK_EYES.map((eye) => (
        <ellipse key={eye.cx} cx={eye.cx} cy={eye.cy} rx={1.9} ry={1.4} fill={accent} />
      ))}
    </svg>
  );
}
