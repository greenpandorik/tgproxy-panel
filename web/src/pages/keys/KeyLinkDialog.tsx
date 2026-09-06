import { Download } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { keyQrUrl, linkKindLabel, useKey, useKeyLinks } from '@/api/keys';
import { useAuth } from '@/auth/AuthProvider';
import { ClientSupportNotice } from '@/components/common/ClientSupportNotice';
import { CopyButton } from '@/components/common/CopyButton';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast';

import { SubscriptionLinkSection } from './SubscriptionLinkSection';

import type { KindLink, NodeLinkGroup } from '@/api/types';

interface KeyLinkDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  keyId: string | null;
}

/** A link, ready to be handed over: the scheme it uses, the URL in mono, and copy. */
function LinkRow({ scheme, value, copyLabel }: { scheme: string; value: string; copyLabel: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="mono w-11 shrink-0 text-xs text-dim">{scheme}</span>
      <Input readOnly value={value} className="mono h-7 text-xs" onFocus={(e) => e.target.select()} aria-label={scheme} />
      <CopyButton value={value} label={copyLabel} className="shrink-0" />
    </div>
  );
}

interface LinkPanelProps {
  keyId: string;
  group: NodeLinkGroup;
  link: KindLink;
  onDownload: (link: KindLink) => void;
}

/**
 * One link of one kind: the QR to scan and the two URLs to paste.
 *
 * The QR sits on a white tile with its quiet zone intact - it is the one
 * deliberately non-neutral surface in the panel, because a code printed on a
 * near-black ground is a code that will not scan. Framing it as a tile rather
 * than a bordered image also says what it is: the thing being handed over.
 */
function LinkPanel({ keyId, group, link, onDownload }: LinkPanelProps) {
  const { t } = useTranslation();

  return (
    <div className="flex gap-3 p-3">
      <img
        src={keyQrUrl(keyId, group.node_id, 256, link.kind)}
        alt={t('keys.link_qr_alt_kind', { hostname: group.hostname, kind: linkKindLabel(link.kind) })}
        width={112}
        height={112}
        className="size-28 shrink-0 rounded-lg bg-white p-3"
      />
      <div className="flex min-w-0 flex-1 flex-col justify-between gap-2">
        <div className="space-y-1.5">
          <LinkRow scheme="t.me" value={link.tme} copyLabel={t('keys.link_copy_tme')} />
          <LinkRow scheme="tg://" value={link.tg} copyLabel={t('keys.link_copy_tg')} />
        </div>
        <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => onDownload(link)}>
          <Download />
          {t('keys.link_download_qr')}
        </Button>
      </div>
    </div>
  );
}

/**
 * Every link one node offers for this key.
 *
 * A telemt node serves two, and they are not variants of one thing: WEB rides
 * inside HTTPS to the node's own site, Fake-TLS is classic MTProto on its own
 * port. Which one a person needs depends on their client and their network, so
 * they get a tab each rather than being stacked - a node's panel stays one screen
 * tall and the QR does not move when you switch. A tproxy node has only the WEB
 * link and says why, in the engine's own word, instead of showing a dead tab.
 */
function NodeLinksCard({
  keyId,
  group,
  onDownload,
}: {
  keyId: string;
  group: NodeLinkGroup;
  onDownload: (group: NodeLinkGroup, link: KindLink) => void;
}) {
  const { t } = useTranslation();
  const multi = group.links.length > 1;
  // A telemt node with a single link has not been given a Fake-TLS domain yet -
  // that is a gap in its configuration, not a property of its engine, so only a
  // tproxy node gets the "this is all there is" note.
  const webOnly = !multi && group.engine === 'tproxy';

  return (
    <div className="rounded-lg border border-hairline">
      <div className="flex items-baseline gap-2 border-b border-hairline px-3 py-2">
        <span className="mono truncate text-xs text-foreground">{group.hostname}</span>
        <span className="truncate text-xs text-dim">{group.node_name}</span>
        {webOnly && <span className="mono ml-auto shrink-0 text-[11px] text-dim">{t('keys.link_web_only')}</span>}
      </div>

      {multi ? (
        <Tabs defaultValue={group.links[0].kind} className="gap-0">
          <TabsList variant="line" className="w-full justify-start px-3">
            {group.links.map((link) => (
              <TabsTrigger key={link.kind} value={link.kind}>
                {linkKindLabel(link.kind)}
              </TabsTrigger>
            ))}
          </TabsList>
          {group.links.map((link) => (
            <TabsContent key={link.kind} value={link.kind}>
              <LinkPanel keyId={keyId} group={group} link={link} onDownload={(l) => onDownload(group, l)} />
            </TabsContent>
          ))}
        </Tabs>
      ) : (
        <LinkPanel keyId={keyId} group={group} link={group.links[0]} onDownload={(l) => onDownload(group, l)} />
      )}
    </div>
  );
}

/**
 * Per-node links + QR for a key.
 *
 * The key itself comes from GET /keys/{id} (status, client support, subscription)
 * and the links from GET /keys/{id}/links, which is writer-only - so a viewer
 * lands on the "no permission" line instead of a 403 in the console.
 */
export function KeyLinkDialog({ open, onOpenChange, keyId }: KeyLinkDialogProps) {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const keyQuery = useKey(keyId ?? '');
  const key = keyQuery.data;
  const revoked = key?.status === 'revoked';
  const linksQuery = useKeyLinks(keyId ?? '', open && isWriter && !!key && !revoked);
  const groups = linksQuery.data?.items ?? [];

  const handleDownload = async (group: NodeLinkGroup, link: KindLink) => {
    if (!keyId || !key) return;
    try {
      const res = await fetch(keyQrUrl(keyId, group.node_id, 256, link.kind), { credentials: 'same-origin' });
      if (!res.ok) throw new Error('qr fetch failed');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${key.label}-${group.hostname}-${link.kind}.png`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch {
      toast.add({ description: t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <Dialog open={open && !!keyId} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{key ? t('keys.link_title', { label: key.label }) : t('keys.link_title_loading')}</DialogTitle>
        </DialogHeader>

        {keyQuery.isLoading ? (
          <Skeleton className="h-40 w-full" />
        ) : !key ? (
          <p className="py-6 text-center text-sm text-mute">{t('common.error_generic')}</p>
        ) : revoked ? (
          <p className="py-6 text-center text-sm text-mute">{t('keys.link_revoked')}</p>
        ) : !isWriter || linksQuery.isError ? (
          <p className="py-6 text-center text-sm text-mute">{t('keys.link_no_permission')}</p>
        ) : linksQuery.isLoading ? (
          <Skeleton className="h-40 w-full" />
        ) : groups.length === 0 ? (
          <p className="py-6 text-center text-sm text-mute">{t('keys.link_no_nodes')}</p>
        ) : (
          <div className="space-y-3">
            {key.status === 'pending' && (
              <p className="flex items-start gap-2 text-xs text-warn">
                <span className="mt-1 size-[7px] shrink-0 rounded-full bg-warn" aria-hidden="true" />
                {t('keys.link_pending_hint')}
              </p>
            )}

            <div className="max-h-[42vh] space-y-3 overflow-y-auto pr-1">
              {groups.map((group) => (
                <NodeLinksCard key={group.node_id} keyId={key.id} group={group} onDownload={(g, link) => void handleDownload(g, link)} />
              ))}
            </div>

            <ClientSupportNotice clientSupport={linksQuery.data?.client_support ?? key.client_support} />

            {/* This branch only renders once the "revoked" and "no links" cases above
                have returned, so the key here is always active/pending - nothing to lock. */}
            <SubscriptionLinkSection key={key.id} keyId={key.id} subscriptionActive={key.subscription_active} isWriter={isWriter} />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
