import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { nodeKeys, useNodes } from '@/api/nodes';
import { DraftBanner } from '@/components/common/DraftBanner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { HelpButton } from '@/help';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { toast } from '@/components/ui/toast';
import { api, ApiError } from '@/lib/api';
import { useDraft } from '@/lib/drafts';

const EMPTY_DRAFT = { node_id: '' };

interface AssignTemplateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  templateId: string;
  templateName: string;
}

/** "Assign to node..." dialog opened from the template editor: picks a node and assigns this template's bundle to it (marks the node dirty, pending the next apply). */
export function AssignTemplateDialog({ open, onOpenChange, templateId, templateName }: AssignTemplateDialogProps) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const nodesQuery = useNodes();
  const [nodeId, setNodeId] = useState('');
  const draft = useDraft(`site-assign-${templateId}`, { node_id: nodeId }, { initial: EMPTY_DRAFT, open });

  const resumeDraft = () => {
    if (!draft.draft) return;
    setNodeId(draft.draft.value.node_id);
    draft.dismiss();
  };

  const assign = useMutation({
    mutationFn: (id: string) => api.post(`/api/v1/nodes/${id}/site`, { template_id: templateId }),
    onSuccess: (_data, id) => {
      void qc.invalidateQueries({ queryKey: nodeKeys.site(id) });
      void qc.invalidateQueries({ queryKey: nodeKeys.all });
    },
  });

  const nodes = nodesQuery.data?.items ?? [];
  const selectedNode = nodes.find((n) => n.id === nodeId);

  const handleAssign = async () => {
    if (!nodeId || !selectedNode) return;
    try {
      await assign.mutateAsync(nodeId);
      toast.add({ description: t('sites.assign_success', { node: selectedNode.name }), type: 'success' });
      draft.clear();
      setNodeId('');
      onOpenChange(false);
    } catch (err) {
      toast.add({ description: err instanceof ApiError ? err.message : t('common.error_generic'), type: 'error' });
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // The pick is kept as a draft, not as live state, so a reopened dialog
        // starts clean and offers it back.
        if (!next) setNodeId('');
        onOpenChange(next);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-1.5">
            <DialogTitle>{t('sites.assign_dialog_title', { name: templateName })}</DialogTitle>
            <HelpButton topic="sites.assign" className="-my-1.5" />
          </div>
          <DialogDescription>{t('sites.assign_dialog_description')}</DialogDescription>
        </DialogHeader>

        {draft.draft && <DraftBanner savedAt={draft.draft.savedAt} onResume={resumeDraft} onDiscard={draft.clear} />}

        {nodesQuery.isLoading ? (
          // The picker in silhouette: a label line over the select it will become.
          <div className="space-y-2">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-8 w-full rounded-control" />
          </div>
        ) : nodesQuery.isError ? (
          <p role="alert" className="text-body text-destructive">
            {nodesQuery.error instanceof ApiError ? nodesQuery.error.message : t('common.error_generic')}
          </p>
        ) : nodes.length === 0 ? (
          <p className="text-body text-mute">{t('sites.assign_no_nodes')}</p>
        ) : (
          <div className="space-y-2">
            <Label htmlFor="assign-node">{t('sites.assign_node_label')}</Label>
            <Select value={nodeId} onValueChange={(v) => setNodeId(v ?? '')}>
              <SelectTrigger id="assign-node" className="w-full">
                {/* Resolve the label explicitly - SelectValue would otherwise show the raw
                    node id (a UUID) until the popup has mounted at least once. */}
                <SelectValue placeholder={t('sites.assign_node_label')}>
                  {(v: string) => nodes.find((n) => n.id === v)?.name ?? t('sites.assign_node_label')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={n.id}>
                    <span className="flex min-w-0 items-baseline gap-2">
                      <span className="truncate">{n.name}</span>
                      <span className="mono truncate text-mono text-dim">{n.hostname}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={assign.isPending}>
            {t('common.cancel')}
          </Button>
          <Button type="button" onClick={() => void handleAssign()} disabled={!nodeId || assign.isPending}>
            {t('sites.assign_submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
