import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Logo, MARK_PATH } from './Logo';

describe('Logo', () => {
  it('is decorative next to the panel name and names itself when asked', () => {
    const { rerender } = render(<Logo />);
    const svg = screen.getByTestId('brand-logo');
    expect(svg).toHaveAttribute('aria-hidden', 'true');
    expect(svg).toHaveAttribute('width', '16');
    expect(svg.querySelector('path')).toHaveAttribute('d', MARK_PATH);
    expect(svg.querySelector('path')).toHaveAttribute('stroke', 'currentColor');
    const eyes = svg.querySelectorAll('ellipse');
    expect(eyes).toHaveLength(2);
    for (const eye of eyes) expect(eye).toHaveAttribute('fill', 'var(--brand-primary)');

    rerender(<Logo size={40} title="TGProxy Panel" accent="#123456" />);
    expect(screen.getByRole('img', { name: 'TGProxy Panel' })).toHaveAttribute('height', '40');
    for (const eye of svg.querySelectorAll('ellipse')) expect(eye).toHaveAttribute('fill', '#123456');
  });
});
