export interface NewLine {
  line: number;
  prefix: string;
  note: string;
}

export function parseBlockLines(text: string): NewLine[] {
  const out: NewLine[] = [];
  text.split(/\r?\n/).forEach((raw, i) => {
    const trimmed = raw.trim();
    if (!trimmed || trimmed.startsWith('#')) return;
    const [prefix, ...rest] = trimmed.split(/[\s,;]+/);
    out.push({ line: i + 1, prefix, note: rest.join(' ').trim() });
  });
  return out;
}
