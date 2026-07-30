import { Trash2, Filter, Info } from "lucide-react";
import { Button, Card } from "@/components/ui";
import { useTunnelStore } from "@/store/tunnel";
import { formatTime, cn } from "@/lib/utils";
import type { LogLevel } from "@/types";

const LOG_COLORS: Record<LogLevel, string> = {
  debug: "text-muted/70",
  info: "text-fg",
  warn: "text-warn",
  error: "text-danger",
};

const FILTERS: Array<{ value: LogLevel | null; label: string }> = [
  { value: null, label: "Все" },
  { value: "info", label: "Info" },
  { value: "debug", label: "Debug" },
  { value: "warn", label: "Warn" },
  { value: "error", label: "Error" },
];

export function Logs() {
  const logs = useTunnelStore((s) => s.logs);
  const logFilter = useTunnelStore((s) => s.logFilter);
  const setLogFilter = useTunnelStore((s) => s.setLogFilter);
  const clearLogs = useTunnelStore((s) => s.clearLogs);
  const status = useTunnelStore((s) => s.status);

  const filtered =
    logFilter === null ? logs : logs.filter((l) => l.level === logFilter);

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      {/* Тулбар */}
      <Card className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Filter size={15} className="text-muted" />
          <div className="flex flex-wrap items-center gap-1">
            {FILTERS.map((f) => (
              <button
                key={f.label}
                onClick={() => setLogFilter(f.value)}
                className={cn(
                  "rounded-lg px-2.5 py-1 text-xs font-medium transition-colors",
                  logFilter === f.value
                    ? "bg-accent/15 text-accent"
                    : "text-muted hover:bg-surface-2 hover:text-fg"
                )}
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[11px] text-muted">{filtered.length} записей</span>
          <Button variant="ghost" size="sm" onClick={clearLogs}>
            <Trash2 size={14} /> Очистить
          </Button>
        </div>
      </Card>

      {/* Лог-поток */}
      <Card className="p-0 overflow-hidden">
        <div className="h-[420px] overflow-y-auto font-mono text-xs leading-6 px-4 py-3">
          {filtered.length === 0 ? (
            <EmptyLog status={status} />
          ) : (
            filtered.map((l) => (
              <div key={l.id} className="flex gap-3 animate-fade-in">
                <span className="shrink-0 text-muted/50">{formatTime(l.ts)}</span>
                <span className={cn("w-11 shrink-0 uppercase font-semibold", LOG_COLORS[l.level])}>
                  {l.level}
                </span>
                <span className={cn("break-all", LOG_COLORS[l.level])}>{l.msg}</span>
              </div>
            ))
          )}
        </div>
      </Card>
    </div>
  );
}

function EmptyLog({ status }: { status: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center text-sm text-muted/60">
      <Info size={28} className="mb-3" />
      <p>
        {status === "connected"
          ? "Логи появятся в процессе работы туннеля"
          : "Подключитесь к туннелю, чтобы увидеть логи"}
      </p>
    </div>
  );
}
