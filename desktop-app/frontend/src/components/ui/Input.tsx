import { forwardRef } from "react";
import type {
  InputHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";
import { cn } from "@/lib/utils";

export const Input = forwardRef<
  HTMLInputElement,
  InputHTMLAttributes<HTMLInputElement>
>(({ className, ...props }, ref) => (
  <input
    ref={ref}
    className={cn(
      "h-9 w-full rounded-xl border border-border bg-surface-2 px-3 text-sm text-fg",
      "placeholder:text-muted/70 transition-colors",
      "focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30",
      "disabled:opacity-50",
      className
    )}
    {...props}
  />
));
Input.displayName = "Input";

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(({ className, ...props }, ref) => (
  <textarea
    ref={ref}
    className={cn(
      "w-full rounded-xl border border-border bg-surface-2 px-3 py-2 text-sm text-fg",
      "placeholder:text-muted/70 transition-colors resize-none",
      "focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30",
      className
    )}
    {...props}
  />
));
Textarea.displayName = "Textarea";

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <label className={cn("block", className)}>
      <span className="label mb-1.5 block">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-[11px] text-muted/70">{hint}</span>}
    </label>
  );
}
