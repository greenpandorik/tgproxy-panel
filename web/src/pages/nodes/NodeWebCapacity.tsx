import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { usePatchNode } from '@/api/nodes';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/lib/api';

import { STANDARD_MAX_CONNECTIONS, STANDARD_WEB_CAPACITY as STANDARD, WEB_CAPACITY_PRESETS, isWebCapacity } from './webCapacity';

import type { Node, WebCapacity } from '@/api/types';

/** The node-wide WEB session ceiling: a preset select, what the server reports now, and what a change costs. */
export function NodeWebCapacity({ node, canEdit }: { node: Node; canEdit: boolean }) {
  const { t, i18n } = useTranslation();
  const patchNode = usePatchNode(node.id);
  const reported = node.health?.web_capacity;
  const saved: WebCapacity = node.web_capacity ?? (isWebCapacity(reported) ? reported : STANDARD);
  const [choice, setChoice] = useState<WebCapacity>(saved);
  const [confirming, setConfirming] = useState(false);
  const [lastSaved, setLastSaved] = useState(saved);
  if (lastSaved !== saved) {
    setLastSaved(saved);
    setChoice(saved);
  }

  const num = (n: number) => n.toLocaleString(i18n.language);
  const optionLabel = (p: (typeof WEB_CAPACITY_PRESETS)[number]) => {
    const text = t('nodes.capacity_option', { people: p.people, connections: num(p.connections) });
    return p.people === STANDARD ? `${text} · ${t('nodes.capacity_standard')}` : text;
  };
  const supported = node.web_capacity_supported === true;
  const editable = canEdit && supported;

  const save = async () => {
    try {
      await patchNode.mutateAsync({ web_capacity: choice });
      toast.add({ description: t('nodes.capacity_saved'), type: 'success' });
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <div className="space-y-3" data-testid="web-capacity">
      <div className="max-w-sm space-y-2">
        <Label htmlFor="node-web-capacity">{t('nodes.capacity_title')}</Label>
        <select
          id="node-web-capacity"
          className="ops-select"
          value={choice}
          disabled={!editable}
          onChange={(e) => setChoice(Number(e.target.value) as WebCapacity)}
        >
          {WEB_CAPACITY_PRESETS.map((p) => (
            <option key={p.people} value={p.people}>
              {optionLabel(p)}
            </option>
          ))}
        </select>
      </div>
      <p className="text-label text-mute" data-testid="web-capacity-current">
        {reported
          ? t('nodes.capacity_current', {
              people: reported,
              connections: num(node.health?.max_connections ?? STANDARD_MAX_CONNECTIONS),
            })
          : t('nodes.capacity_current_unknown')}
      </p>
      {canEdit && !supported && (
        <p className="text-label text-warn" data-testid="web-capacity-unsupported">
          {t('nodes.capacity_unsupported')}
        </p>
      )}
      {editable && (
        <>
          <p className="flex max-w-[72ch] items-start gap-2 text-label text-warn">
            <span className="mt-1 size-[7px] shrink-0 rounded-pill bg-warn" aria-hidden="true" />
            {t('nodes.capacity_restart_note')}
          </p>
          <p className="max-w-[72ch] text-label text-mute">{t('nodes.capacity_memory_note')}</p>
          <Button type="button" disabled={choice === saved || patchNode.isPending} onClick={() => setConfirming(true)}>
            {t('common.save')}
          </Button>
        </>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t('nodes.capacity_confirm_title')}
        description={t('nodes.capacity_restart_note')}
        confirmLabel={t('common.save')}
        onConfirm={save}
      />
    </div>
  );
}
