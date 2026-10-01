import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors } from '@dnd-kit/core';
import {
  arrayMove,
  rectSortingStrategy,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { GripVertical } from 'lucide-react';

import { cn } from '@/lib/utils';

import type { Announcements, DragEndEvent, UniqueIdentifier } from '@dnd-kit/core';
import type { SortableHandle } from './sortableItem';
import type { ReactNode } from 'react';

/** What a screen reader hears while an item moves. Every string comes from the caller's locale. */
export interface SortableMessages {
  instructions: string;
  picked: (name: string, position: number, total: number) => string;
  moved: (name: string, position: number, total: number) => string;
  dropped: (name: string, position: number, total: number) => string;
  cancelled: (name: string) => string;
}

interface SortableListProps {
  /** Item ids in their current order. */
  items: string[];
  onReorder: (next: string[]) => void;
  /** A list moves items up and down; a grid moves them in both directions. */
  layout?: 'list' | 'grid';
  /** The item's name, for the announcements. */
  nameOf: (id: string) => string;
  messages: SortableMessages;
  children: ReactNode;
}

/** Reorders its items by dragging a handle, with the pointer or with Space and the arrow keys. */
export function SortableList({ items, onReorder, layout = 'list', nameOf, messages, children }: SortableListProps) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const position = (id: UniqueIdentifier) => items.indexOf(String(id)) + 1;
  const announcements: Announcements = {
    onDragStart: ({ active }) => messages.picked(nameOf(String(active.id)), position(active.id), items.length),
    onDragOver: ({ active, over }) =>
      over ? messages.moved(nameOf(String(active.id)), position(over.id), items.length) : undefined,
    onDragEnd: ({ active, over }) =>
      over
        ? messages.dropped(nameOf(String(active.id)), position(over.id), items.length)
        : messages.cancelled(nameOf(String(active.id))),
    onDragCancel: ({ active }) => messages.cancelled(nameOf(String(active.id))),
  };

  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const from = items.indexOf(String(active.id));
    const to = items.indexOf(String(over.id));
    if (from < 0 || to < 0) return;
    onReorder(arrayMove(items, from, to));
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={handleDragEnd}
      accessibility={{ announcements, screenReaderInstructions: { draggable: messages.instructions } }}
    >
      <SortableContext items={items} strategy={layout === 'grid' ? rectSortingStrategy : verticalListSortingStrategy}>
        {children}
      </SortableContext>
    </DndContext>
  );
}

/** The grip an item is dragged by. It takes focus, so Space picks the item up from the keyboard. */
export function DragHandle({
  activator,
  attributes,
  listeners,
  label,
  className,
}: SortableHandle & { label: string; className?: string }) {
  return (
    <button
      type="button"
      ref={activator}
      {...attributes}
      {...listeners}
      aria-label={label}
      title={label}
      className={cn(
        'relative z-10 inline-flex size-8 shrink-0 cursor-grab touch-none items-center justify-center rounded-control text-mute transition-colors outline-none hover:bg-elevated hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing pointer-coarse:size-11',
        className,
      )}
    >
      <GripVertical className="size-4" aria-hidden="true" />
    </button>
  );
}
