/** Pick readable text for operator-supplied hex colors, including short hex. */
export function brandForeground(color: string): '#ffffff' | '#000000' {
  let hex = color.replace(/^#/, '');
  if (hex.length === 3) hex = [...hex].map((c) => c + c).join('');
  if (!/^[0-9a-f]{6}$/i.test(hex)) return '#ffffff';
  const linear = [0, 2, 4].map((i) => {
    const value = parseInt(hex.slice(i, i + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  const luminance = linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
  return 1.05 / (luminance + 0.05) >= 4.5 ? '#ffffff' : '#000000';
}
