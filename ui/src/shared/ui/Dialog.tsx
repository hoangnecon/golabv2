import { X } from 'lucide-react';
import { useEffect, useId, useRef } from 'react';
import type { PointerEvent, ReactNode } from 'react';
import { cn } from '@/shared/lib/cn';
import { IconButton } from './IconButton';

interface DialogProps {
  title: string;
  onClose: () => void;
  wide?: boolean;
  children: ReactNode;
}

/**
 * Native modal <dialog> opened via showModal():
 * - No redundant role="dialog" or aria-modal (native modal semantics apply).
 * - Declarative `closedby="any"` plus outside press+release bounding-box fallback
 *   so text-selection drags ending on the backdrop never accidentally close the dialog.
 */
export function Dialog({ title, onClose, wide, children }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const pressOutsideRef = useRef(false);
  const titleId = useId();

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    element.setAttribute('closedby', 'any');
    if (!element.open) {
      element.showModal();
    }
  }, []);

  const isOutsideBox = (dialog: HTMLDialogElement, clientX: number, clientY: number) => {
    const rect = dialog.getBoundingClientRect();
    return (
      clientX < rect.left ||
      clientX > rect.right ||
      clientY < rect.top ||
      clientY > rect.bottom
    );
  };

  const handlePointerDown = (event: PointerEvent<HTMLDialogElement>) => {
    const dialog = event.currentTarget;
    pressOutsideRef.current =
      event.target === dialog && isOutsideBox(dialog, event.clientX, event.clientY);
  };

  const handlePointerUp = (event: PointerEvent<HTMLDialogElement>) => {
    const dialog = event.currentTarget;
    if (!pressOutsideRef.current) return;
    pressOutsideRef.current = false;
    if ('closedBy' in HTMLDialogElement.prototype) return;
    if (event.target === dialog && isOutsideBox(dialog, event.clientX, event.clientY)) {
      dialog.close();
    }
  };

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      onPointerDown={handlePointerDown}
      onPointerUp={handlePointerUp}
      className={cn(
        'm-auto w-11/12 rounded-xl border-0 bg-surface p-0 text-fg shadow-overlay',
        wide ? 'max-w-xl' : 'max-w-md',
      )}
    >
      <div className="flex h-14 shrink-0 items-center justify-between border-b border-line pl-6 pr-3">
        <h2 id={titleId} className="text-title font-semibold">
          {title}
        </h2>
        <IconButton label="Close" onClick={() => ref.current?.close()}>
          <X aria-hidden className="size-4" strokeWidth={1.75} />
        </IconButton>
      </div>
      <div className="scroll-pane min-h-0 flex-1 overflow-y-auto px-6 pb-6 pt-4">{children}</div>
    </dialog>
  );
}

export function DialogActions({ children }: { children: ReactNode }) {
  return (
    <div className="-mx-6 -mb-6 mt-2 flex justify-end gap-2 rounded-b-xl border-t border-line bg-raised px-6 py-4">
      {children}
    </div>
  );
}
