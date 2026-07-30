import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Pause, Play, Filter } from "lucide-react";
import { api, type LogEntry } from "@/lib/api";
import { formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";

type Level = "DEBUG" | "INFO" | "WARN" | "ERROR";

const LEVEL_COLOR: Record<string, string> = {
  DEBUG: "text-muted-foreground/70",
  INFO: "text-foreground",
  WARN: "text-amber-500 dark:text-amber-400",
  ERROR: "text-destructive",
};

const FILTERS: Array<{ value: Level | null; label: string }> = [
  { value: null, label: "Все" },
  { value: "INFO", label: "Info" },
  { value: "DEBUG", label: "Debug" },
  { value: "WARN", label: "Warn" },
  { value: "ERROR", label: "Error" },
];

// normLevel приводит уровень slog ("INFO", "WARN"...) к нашему ключу.
function normLevel(l: string): string {
  return l.toUpperCase().replace(/[^A-Z]/g, "").slice(0, 5);
}

export function Logs() {
  const [live, setLive] = React.useState(true);
  const [filter, setFilter] = React.useState<Level | null>(null);
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const atBottomRef = React.useRef(true);

  const { data } = useQuery({
    queryKey: ["logs"],
    queryFn: () => api.logs(1000),
    refetchInterval: live ? 2000 : false,
  });

  const entries: LogEntry[] = data ?? [];
  const filtered = filter ? entries.filter((e) => normLevel(e.level) === filter) : entries;

  // Автопрокрутка вниз, если пользователь уже был у низа.
  React.useEffect(() => {
    const el = scrollRef.current;
    if (el && atBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [filtered.length]);

  return (
    <div className="mx-auto max-w-5xl space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Логи</h1>
          <p className="text-sm text-muted-foreground">Последние записи журнала сервера</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setLive((v) => !v)}>
          {live ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
          {live ? "Пауза" : "Возобновить"}
        </Button>
      </div>

      <Card className="flex items-center justify-between px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Filter className="h-4 w-4 text-muted-foreground" />
          <div className="flex items-center gap-1">
            {FILTERS.map((f) => (
              <button
                key={f.label}
                onClick={() => setFilter(f.value)}
                className={cn(
                  "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                  filter === f.value
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground"
                )}
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>
        <span className="text-xs text-muted-foreground">{filtered.length} записей</span>
      </Card>

      <Card className="overflow-hidden p-0">
        <div
          ref={scrollRef}
          onScroll={(e) => {
            const el = e.currentTarget;
            atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
          }}
          className="h-[540px] overflow-y-auto px-4 py-3 font-mono text-xs leading-6"
        >
          {filtered.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              Записей пока нет
            </div>
          ) : (
            filtered.map((l, i) => {
              const lvl = normLevel(l.level);
              return (
                <div key={i} className="flex gap-3">
                  <span className="shrink-0 text-muted-foreground/50">{formatTime(l.ts)}</span>
                  <span
                    className={cn("w-12 shrink-0 font-semibold uppercase", LEVEL_COLOR[lvl] ?? "text-foreground")}
                  >
                    {lvl}
                  </span>
                  <span className={cn("break-all", LEVEL_COLOR[lvl] ?? "text-foreground")}>{l.msg}</span>
                </div>
              );
            })
          )}
        </div>
      </Card>
    </div>
  );
}
