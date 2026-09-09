import { OFFLINE_SERIES_COLOR } from '@/lib/chart';

/** The mark's own coordinate space. Width matches the column the mockup gives it. */
const W = 76;
const H = 22;
/** Half the stroke plus a hair, so a peak at the top of the range is not clipped. */
const PAD = 2;

export interface SparklineProps {
  /** The series, oldest first. Fewer than two points cannot draw a line. */
  points: number[];
  /** The colour this node already has in the chart above the table. */
  color: string;
  /** A node that stopped reporting: flat, dashed, --dim. Not a line at zero. */
  offline?: boolean;
}

/** `points` mapped into the box, with a flat series pinned to the middle. */
function path(points: number[]): string {
  const max = Math.max(...points);
  const min = Math.min(...points);
  const span = max - min;
  const step = W / (points.length - 1);
  return points
    .map((v, i) => {
      const x = i * step;
      const y = span === 0 ? H / 2 : PAD + (1 - (v - min) / span) * (H - PAD * 2);
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(' ');
}

export function Sparkline({ points, color, offline }: SparklineProps) {
  const flat = offline || points.length < 2;

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      width={W}
      height={H}
      fill="none"
      preserveAspectRatio="none"
      aria-hidden="true"
      className="block overflow-visible"
    >
      <path
        d={flat ? `M0 ${H / 2} L${W} ${H / 2}` : path(points)}
        stroke={flat ? OFFLINE_SERIES_COLOR : color}
        strokeWidth={1.4}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeDasharray={flat ? '2 3' : undefined}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
