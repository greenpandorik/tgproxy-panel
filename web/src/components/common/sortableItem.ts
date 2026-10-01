import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';

import type { CSSProperties } from 'react';

/** What a DragHandle needs from its item. */
export interface SortableHandle {
  activator: ReturnType<typeof useSortable>['setActivatorNodeRef'];
  attributes: ReturnType<typeof useSortable>['attributes'];
  listeners: ReturnType<typeof useSortable>['listeners'];
}

/** One item of a SortableList: the ref setter and style for its box, and the props for its handle. */
export function useSortableItem(id: string, disabled = false) {
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging } = useSortable({
    id,
    disabled,
  });
  const style: CSSProperties = {
    transform: CSS.Translate.toString(transform),
    transition,
    position: isDragging ? 'relative' : undefined,
    zIndex: isDragging ? 20 : undefined,
  };
  return {
    setNode: setNodeRef,
    style,
    dragging: isDragging,
    handle: { activator: setActivatorNodeRef, attributes, listeners } satisfies SortableHandle,
  };
}
