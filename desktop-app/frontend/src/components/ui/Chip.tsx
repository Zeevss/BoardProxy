import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

export function Chip({
  className,
  children,
  onRemove,
  ...props
}: HTMLAttributes<HTMLSpanElement> & { onRemove?: () => void }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-lg bg-surface-2 px-2.5 py-1 text-xs font-medium text-fg border border-border",
        className
      )}
      {...props}
    >
      {children}
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="text-muted hover:text-danger transition-colors"
          aria-label="Удалить"
        >
          ×
        </button>
      )}
    </span>
  );
}
