import { Ban, Download, Lock, RefreshCw, Server, TriangleAlert } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { keyQrUrl, linkKindLabel, useKey, useKeyLinks } from '@/api/keys';
import { useAuth } from '@/auth/AuthProvider';
import { AdvancedSettings } from '@/components/common/AdvancedSettings';
import { ClientSupportNotice } from '@/components/common/ClientSupportNotice';
import { CopyButton } from '@/components/common/CopyButton';
import { EmptyState } from '@/components/common/EmptyState';
import { SegmentedControl } from '@/components/common/SegmentedControl';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { enter } from '@/components/ui/motion';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast';
import { HelpButton } from '@/help';
import { cn } from '@/lib/utils';

import { SubscriptionLinkSection } from './SubscriptionLinkSection';

import type { KindLink, NodeLinkGroup } from '@/api/types';

interface KeyLinkDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  keyId: string | null;
  /** Opened right after creation: handing the access over comes first, per-node links second. */
  handover?: boolean;
}

function LinkRow({ scheme, value, copyLabel }: { scheme: string; value: string; copyLabel: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className="mono w-11 shrink-0 text-mono text-mute">{scheme}</span>
      <Input readOnly value={value} className="mono text-mono" onFocus={(e) => e.target.select()} aria-label={scheme} />
      <CopyButton
        value={value}
        label={copyLabel}
        className="size-8 shrink-0 border-hairline-strong bg-surface text-foreground hover:bg-elevated"
      />
    </div>
  );
}

function LinksSkeleton() {
  return (
    <div className="space-y-4">
      {[0, 1].map((i) => (
        <div key={i} className="rounded-surface border border-hairline">
          <div className="flex items-center gap-3 border-b border-hairline px-4 py-3">
            <Skeleton className="h-3 w-40" />
            <Skeleton className="h-3 w-20" />
          </div>
          <div className="flex gap-4 p-4">
            <Skeleton className="size-28 shrink-0" />
            <div className="flex min-w-0 flex-1 flex-col justify-between gap-4">
              <div className="space-y-2">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </div>
              <Skeleton className="h-7 w-28" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

interface LinkPanelProps {
  keyId: string;
  group: NodeLinkGroup;
  link: KindLink;
  onDownload: (link: KindLink) => void;
}

function LinkPanel({ keyId, group, link, onDownload }: LinkPanelProps) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-4 p-4 sm:flex-row">
      <img
        src={keyQrUrl(keyId, group.node_id, 256, link.kind, link.domain)}
        alt={t('keys.link_qr_alt_kind', {
          hostname: group.hostname,
          kind: link.domain ? `${linkKindLabel(link.kind)} ${link.domain}` : linkKindLabel(link.kind),
        })}
        width={112}
        height={112}
        className="size-28 shrink-0 self-start rounded-surface bg-white p-3"
      />
      <div className="flex min-w-0 flex-1 flex-col justify-between gap-4">
        <div className="space-y-2">
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

/** The links of one kind on one node; several Fake-TLS domains get a switcher above the link. */
function KindLinks({ keyId, group, links, onDownload }: Omit<LinkPanelProps, 'link'> & { links: KindLink[] }) {
  const { t } = useTranslation();
  const [domain, setDomain] = useState(links[0].domain ?? '');
  const link = links.find((l) => (l.domain ?? '') === domain) ?? links[0];

  return (
    <>
      {links.length > 1 && (
        <div className="flex flex-wrap items-center gap-2 px-4 pt-4">
          <span className="text-label text-mute">{t('keys.link_tls_domain')}</span>
          <SegmentedControl
            value={domain}
            options={links.map((l) => ({ value: l.domain ?? '', label: l.domain ?? '' }))}
            onChange={setDomain}
            label={t('keys.link_tls_domain')}
            className="max-w-full flex-wrap"
          />
        </div>
      )}
      <LinkPanel keyId={keyId} group={group} link={link} onDownload={onDownload} />
    </>
  );
}

function NodeLinksCard({
  keyId,
  group,
  index,
  onDownload,
}: {
  keyId: string;
  group: NodeLinkGroup;
  index: number;
  onDownload: (group: NodeLinkGroup, link: KindLink) => void;
}) {
  const { t } = useTranslation();
  const kinds = [...new Set(group.links.map((link) => link.kind))];
  const multi = kinds.length > 1;
  const webOnly = !multi && group.engine === 'tproxy';
  const arrival = enter(index);

  return (
    <div className={cn(arrival.className, 'overflow-hidden rounded-surface border border-hairline')} style={arrival.style}>
      <div className="flex items-baseline gap-3 border-b border-hairline px-4 py-3">
        <span className="mono truncate text-mono text-foreground">{group.hostname}</span>
        <span className="truncate text-label text-mute">{group.node_name}</span>
        {webOnly && <span className="micro ml-auto shrink-0 text-mute">{t('keys.link_web_only')}</span>}
      </div>

      {multi ? (
        <Tabs defaultValue={kinds[0]} className="gap-0">
          {/*
            The two kinds are not variants of one link, and the tab row is what
            says so: it spans the card as its own band on --bg-3, with the
            panel's 2px active marker under the chosen label and the engine's
            own word on it (WEB / Fake-TLS). An operator picking a link for a
            phone should not have to read a URL to see which of the two is
            selected.
          */}
          <TabsList variant="line" className="w-full justify-start gap-4 bg-elevated px-4">
            {kinds.map((kind) => (
              <TabsTrigger key={kind} value={kind}>
                {linkKindLabel(kind)}
              </TabsTrigger>
            ))}
          </TabsList>
          {kinds.map((kind) => (
            <TabsContent key={kind} value={kind}>
              <KindLinks
                keyId={keyId}
                group={group}
                links={group.links.filter((link) => link.kind === kind)}
                onDownload={(l) => onDownload(group, l)}
              />
            </TabsContent>
          ))}
        </Tabs>
      ) : (
        <KindLinks keyId={keyId} group={group} links={group.links} onDownload={(l) => onDownload(group, l)} />
      )}
    </div>
  );
}

// Per-node links + QR for a key.
export function KeyLinkDialog({ open, onOpenChange, keyId, handover }: KeyLinkDialogProps) {
  const { t } = useTranslation();
  const { isWriter } = useAuth();
  const keyQuery = useKey(keyId ?? '');
  const key = keyQuery.data;
  const revoked = key?.status === 'revoked';
  const linksQuery = useKeyLinks(keyId ?? '', open && isWriter && !!key && !revoked);
  const groups = linksQuery.data?.items ?? [];
  const bothKinds = groups.some((g) => new Set(g.links.map((l) => l.kind)).size > 1);
  const whichHint = bothKinds && <p className="text-label text-mute">{t('keys.link_which_hint')}</p>;

  const handleDownload = async (group: NodeLinkGroup, link: KindLink) => {
    if (!keyId || !key) return;
    try {
      const res = await fetch(keyQrUrl(keyId, group.node_id, 256, link.kind, link.domain), { credentials: 'same-origin' });
      if (!res.ok) throw new Error('qr fetch failed');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${key.label}-${group.hostname}-${link.kind}${link.domain && link.domain !== group.hostname ? `-${link.domain}` : ''}.png`;
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
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <div className="flex items-center gap-2 pr-6">
            <DialogTitle>
              {!key
                ? t('keys.link_title_loading')
                : handover
                  ? t('keys.link_created_title', { label: key.label })
                  : t('keys.link_title', { label: key.label })}
            </DialogTitle>
            <HelpButton topic="keys.link" className="-my-1" />
          </div>
        </DialogHeader>

        {keyQuery.isLoading ? (
          <LinksSkeleton />
        ) : !key ? (
          <EmptyState
            className="py-10"
            icon={TriangleAlert}
            title={t('common.error_generic')}
            action={
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={keyQuery.isFetching}
                onClick={() => void keyQuery.refetch()}
              >
                <RefreshCw className={cn(keyQuery.isFetching && 'animate-spin')} />
                {t('common.refresh')}
              </Button>
            }
          />
        ) : revoked ? (
          <EmptyState className="py-10" icon={Ban} title={t('keys.link_revoked')} />
        ) : !isWriter ? (
          <EmptyState className="py-10" icon={Lock} title={t('keys.link_no_permission')} />
        ) : linksQuery.isError ? (
          <EmptyState
            className="py-10"
            icon={TriangleAlert}
            title={t('common.error_generic')}
            action={
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={linksQuery.isFetching}
                onClick={() => void linksQuery.refetch()}
              >
                <RefreshCw className={cn(linksQuery.isFetching && 'animate-spin')} />
                {t('common.refresh')}
              </Button>
            }
          />
        ) : linksQuery.isLoading ? (
          <LinksSkeleton />
        ) : groups.length === 0 ? (
          <EmptyState className="py-10" icon={Server} title={t('keys.link_no_nodes')} />
        ) : (
          <div className="space-y-4">
            {key.status === 'pending' && (
              <p className="flex items-start gap-2 text-label text-warn">
                <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
                {t('keys.link_pending_hint')}
              </p>
            )}

            {handover ? (
              <>
                {/* This branch only renders once the "revoked" and "no links" cases above
                    have returned, so the key here is always active/pending - nothing to lock. */}
                <SubscriptionLinkSection
                  key={key.id}
                  keyId={key.id}
                  subscriptionActive={key.subscription_active}
                  isWriter={isWriter}
                  className="border-t-0 pt-0"
                />

                <AdvancedSettings label={t('keys.link_per_node_toggle')} className="border-t border-hairline pt-4">
                  {whichHint}
                  <div className="max-h-[38vh] space-y-4 overflow-y-auto pr-1">
                    {groups.map((group, i) => (
                      <NodeLinksCard
                        key={group.node_id}
                        keyId={key.id}
                        group={group}
                        index={i}
                        onDownload={(g, link) => void handleDownload(g, link)}
                      />
                    ))}
                  </div>
                  <ClientSupportNotice clientSupport={linksQuery.data?.client_support ?? key.client_support} />
                </AdvancedSettings>
              </>
            ) : (
              <>
                {whichHint}
                <div className="max-h-[42vh] space-y-4 overflow-y-auto pr-1">
                  {groups.map((group, i) => (
                    <NodeLinksCard
                      key={group.node_id}
                      keyId={key.id}
                      group={group}
                      index={i}
                      onDownload={(g, link) => void handleDownload(g, link)}
                    />
                  ))}
                </div>

                <ClientSupportNotice clientSupport={linksQuery.data?.client_support ?? key.client_support} />

                <SubscriptionLinkSection
                  key={key.id}
                  keyId={key.id}
                  subscriptionActive={key.subscription_active}
                  isWriter={isWriter}
                />
              </>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
