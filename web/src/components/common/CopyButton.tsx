import { CheckIcon, CopyIcon } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { toast } from '@/components/ui/toast';
import { cn } from '@/lib/utils';

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to the execCommand fallback below
  }
  try {
    const el = document.createElement('textarea');
    el.value = text;
    el.style.position = 'fixed';
    el.style.opacity = '0';
    document.body.appendChild(el);
    el.focus();
    el.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(el);
    return ok;
  } catch {
    return false;
  }
}

interface CopyButtonProps {
  value: string;
  className?: string;
  label?: string;
  /** Spell the label out beside the icon, as an outline button - for footers where it stands next to other labelled actions. */
  showLabel?: boolean;
}

/** Icon button that copies `value` to the clipboard and briefly confirms with a check mark + toast. */
export function CopyButton({ value, className, label, showLabel }: CopyButtonProps) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const handleClick = async () => {
    const ok = await copyToClipboard(value);
    if (ok) {
      setCopied(true);
      toast.add({ description: t('common.copied'), type: 'success', timeout: 1500 });
      window.setTimeout(() => setCopied(false), 1500);
    } else {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <Button
      type="button"
      variant={showLabel ? 'outline' : 'ghost'}
      size={showLabel ? 'default' : 'icon-sm'}
      className={cn(!showLabel && 'text-muted-foreground hover:text-foreground', className)}
      onClick={() => void handleClick()}
      aria-label={showLabel ? undefined : (label ?? t('common.copy'))}
    >
      {copied ? <CheckIcon className="text-online" /> : <CopyIcon />}
      {showLabel && (label ?? t('common.copy'))}
    </Button>
  );
}
