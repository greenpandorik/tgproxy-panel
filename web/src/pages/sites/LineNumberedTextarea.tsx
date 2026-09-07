import { useRef } from 'react';

import { cn } from '@/lib/utils';

interface LineNumberedTextareaProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  'aria-label'?: string;
  'aria-invalid'?: boolean;
}

/*
 * The mono role's size on 20px rows. Source is the one thing here the operator
 * reads a page of at a time, so it is set in the machine face like every other
 * machine value, on a row taller than the role's own leading to keep a page of
 * code from packing solid. The gutter and the textarea must share these exact
 * values or the numbers drift away from their lines.
 */
const ROW_STYLE = { fontSize: 'var(--t-mono)', lineHeight: '20px' };

/**
 * Plain `<textarea>` with a CSS-counter line-number gutter kept in sync by
 * scrollTop (no code-editor dependency, per the phase 1 ruling). `white-space:
 * pre` keeps the textarea from soft-wrapping so one logical line always maps
 * to exactly one gutter row.
 */
export function LineNumberedTextarea({ value, onChange, placeholder, className, ...aria }: LineNumberedTextareaProps) {
  const gutterRef = useRef<HTMLDivElement>(null);
  const lineCount = value.length === 0 ? 1 : value.split('\n').length;

  return (
    // A fixed height on this row is required: both children are flex items with the
    // default `align-items: stretch`, and without an explicit height here they stretch
    // to fit the *content* (every line-number row), growing the whole page instead of
    // scrolling internally - each side's own `overflow-auto`/`overflow-hidden` only
    // clips once the box's height is actually bounded. The caller supplies it.
    //
    // The editor is a control in the shape lock, so it takes the control radius and
    // the same recessed treatment as every input: --bg inside its --bg-2 panel, a
    // hairline outline, and a page of source reading as a hole in the surface.
    <div className={cn('mono flex overflow-hidden rounded-control border border-hairline-strong bg-background', className)}>
      <div
        ref={gutterRef}
        aria-hidden="true"
        className="tgwp-editor-gutter shrink-0 overflow-hidden border-r border-hairline py-2 pr-2 pl-3 text-right text-dim select-none"
        style={ROW_STYLE}
      >
        {Array.from({ length: lineCount }, (_, i) => (
          <div key={i} className="tgwp-editor-gutter-row" style={ROW_STYLE} />
        ))}
      </div>
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onScroll={(e) => {
          if (gutterRef.current) gutterRef.current.scrollTop = e.currentTarget.scrollTop;
        }}
        placeholder={placeholder}
        spellCheck={false}
        style={ROW_STYLE}
        className="flex-1 resize-none overflow-auto bg-transparent px-3 py-2 whitespace-pre text-foreground outline-none placeholder:text-mute"
        {...aria}
      />
    </div>
  );
}
