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

// The mono role's size on 20px rows.
const ROW_STYLE = { fontSize: 'var(--t-mono)', lineHeight: '20px' };

export function LineNumberedTextarea({ value, onChange, placeholder, className, ...aria }: LineNumberedTextareaProps) {
  const gutterRef = useRef<HTMLDivElement>(null);
  const lineCount = value.length === 0 ? 1 : value.split('\n').length;

  return (
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
