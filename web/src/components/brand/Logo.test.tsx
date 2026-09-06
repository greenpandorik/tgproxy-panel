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
    expect(svg.querySelector('rect')).toHaveAttribute('fill', 'var(--brand-primary)');

    rerender(<Logo size={40} title="TGProxy Panel" accent="#123456" />);
    expect(screen.getByRole('img', { name: 'TGProxy Panel' })).toHaveAttribute('height', '40');
    expect(svg.querySelector('rect')).toHaveAttribute('fill', '#123456');
  });
});
