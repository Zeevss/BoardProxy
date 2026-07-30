import { cn } from "@/lib/utils";

type Tone = "ok" | "warn" | "danger" | "muted" | "accent";

const TONES: Record<Tone, string> = {
  ok: "bg-ok",
  warn: "bg-warn",
  danger: "bg-danger",
  muted: "bg-muted",
  accent: "bg-accent",
};

export function StatusDot({
  tone = "muted",
  pulse = false,
  className,
}: {
  tone?: Tone;
  pulse?: boolean;
  className?: string;
}) {
  return (
    <span className={cn("relative inline-flex h-2.5 w-2.5", className)}>
      {pulse && (
        <span
          className={cn(
            "absolute inline-flex h-full w-full rounded-full opacity-60 animate-pulse-ring",
            TONES[tone]
          )}
        />
      )}
      <span className={cn("relative inline-flex h-2.5 w-2.5 rounded-full", TONES[tone])} />
    </span>
  );
}
