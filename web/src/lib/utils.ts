import { clsx, type ClassValue } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

/*
 * tailwind-merge has to be told about the type scale.
 *
 * The panel's font sizes are named roles (text-display, text-title, text-body,
 * text-label, text-micro, text-mono), not Tailwind's t-shirt sizes, so a stock
 * twMerge does not recognise them as font sizes. It files them under text
 * colour instead, and then drops whichever of the two came first: a button
 * built from `text-background` plus a `text-label` size shipped without its
 * colour, so a white label sat on a white fill and read as an empty button.
 *
 * Registering the roles in the font-size group makes size and colour two
 * separate decisions again, which is what every call site already assumes.
 */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      'font-size': [{ text: ['display', 'title', 'body', 'label', 'micro', 'mono'] }],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
