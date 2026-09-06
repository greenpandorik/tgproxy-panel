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
 * 12.5px mono on 20px rows. The size is a hair below the panel's body text on
 * purpose: source is the one thing here the operator reads a page of at a
 * time, so it gets its own measure rather than the 13px used for prose.
 * The gutter and the textarea must share these exact values or the numbers
 * drift away from their lines.
 */
const ROW_STYLE = { fontSize: '12.5px', lineHeight: '20px' };

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
    // clips once the box's height is actually bounded.
    //
    // The editor sits on --bg inside its --bg-2 panel: the same recess every
    // other input in the panel uses, so a page of source reads as a hole in the
    // surface rather than a second raised card.
    <div className={cn('mono flex h-[28rem] overflow-hidden bg-background', className)}>
      <div
        ref={gutterRef}
        aria-hidden="true"
        className="tgwp-editor-gutter shrink-0 overflow-hidden border-r border-hairline py-2.5 pr-2.5 pl-3.5 text-right text-dim select-none"
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
        className="flex-1 resize-none overflow-auto bg-transparent py-2.5 pr-3.5 pl-3 whitespace-pre text-foreground outline-none placeholder:text-dim"
        {...aria}
      />
    </div>
  );
}
