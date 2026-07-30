import { useMemo } from "react";
import {
  ArrowUpRight,
  ArrowDownRight,
  Globe,
  Server,
  Zap,
  Activity,
} from "lucide-react";
import { Card, CardHeader, CardTitle, StatusDot } from "@/components/ui";
import { useStreamsStore } from "@/store/streams";
import { useTunnelStore } from "@/store/tunnel";
import { formatBytes, formatRate, cn } from "@/lib/utils";
import type { TcpStream } from "@/types";

export function Stats() {
  const streams = useStreamsStore((s) => s.streams);
  const totalCreated = useStreamsStore((s) => s.totalCreated);
  const status = useTunnelStore((s) => s.status);

  const activeCount = useMemo(
    () => streams.filter((s) => s.active).length,
    [streams]
  );
  const totalUp = useMemo(
    () => streams.reduce((acc, s) => acc + s.totalUp, 0),
    [streams]
  );
  const totalDown = useMemo(
    () => streams.reduce((acc, s) => acc + s.totalDown, 0),
    [streams]
  );

  const isOn = status === "connected";

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      {/* Сводка */}
      <div className="grid grid-cols-1 gap-3 min-[620px]:grid-cols-3 min-[900px]:gap-4">
        <SummaryCard
          icon={<Activity size={16} />}
          label="Активных стримов"
          value={isOn ? String(activeCount) : "—"}
          tone="accent"
        />
        <SummaryCard
          icon={<ArrowUpRight size={16} />}
          label="Отправлено"
          value={formatBytes(totalUp)}
        />
        <SummaryCard
          icon={<ArrowDownRight size={16} />}
          label="Получено"
          value={formatBytes(totalDown)}
        />
      </div>

      {/* Список стримов */}
      <Card className="p-0 overflow-hidden">
        <CardHeader className="px-5 py-3.5 border-b border-border mb-0">
          <CardTitle>TCP-стримы</CardTitle>
          <span className="text-xs text-muted">
            всего за сессию: {totalCreated}
          </span>
        </CardHeader>

        {!isOn ? (
          <EmptyState />
        ) : streams.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-sm text-muted/70">
            Ожидание соединений…
          </div>
        ) : (
          <div className="max-h-[440px] overflow-y-auto">
            {/* Заголовок таблицы */}
            <div className="grid grid-cols-[1fr_auto_auto] gap-4 border-b border-border/60 px-5 py-2 text-[11px] font-medium uppercase tracking-wide text-muted/70">
              <span>Цель</span>
              <span className="w-24 text-right">↑ Отправлено</span>
              <span className="w-24 text-right">↓ Получено</span>
            </div>

            <ul>
              {streams.map((s) => (
                <StreamRow key={s.id} stream={s} />
              ))}
            </ul>
          </div>
        )}
      </Card>
    </div>
  );
}

/* ---------- Строка стрима ---------- */

function StreamRow({ stream }: { stream: TcpStream }) {
  // Порт целевого адреса (target = "ip:port").
  const port = stream.target.includes(":")
    ? stream.target.slice(stream.target.lastIndexOf(":") + 1)
    : "";
  // Если известен домен из DNS — показываем его, IP-адрес уходит во вторичную
  // строку. Иначе показываем сам target.
  const primary = stream.host ? `${stream.host}${port ? ":" + port : ""}` : stream.target;
  const secondary = stream.host ? stream.target : "";
  const Icon = stream.host ? Globe : Server;

  return (
    <li
      className={cn(
        "grid grid-cols-[1fr_auto_auto] items-center gap-4 border-b border-border/40 px-5 py-2.5 transition-all duration-500",
        stream.active
          ? "animate-fade-in opacity-100"
          : "opacity-40 grayscale"
      )}
    >
      {/* Цель + статус */}
      <div className="flex min-w-0 items-center gap-2.5">
        <StatusDot
          tone={stream.active ? "ok" : "muted"}
          pulse={stream.active}
          className="shrink-0"
        />
        <Icon
          size={14}
          className={cn("shrink-0", stream.active ? "text-accent" : "text-muted")}
        />
        <span className="flex min-w-0 flex-col">
          <span className="truncate font-mono text-xs text-fg">{primary}</span>
          {secondary && (
            <span className="truncate font-mono text-[10px] text-muted">
              {secondary}
            </span>
          )}
        </span>
        {stream.active && (stream.rateDown > 200_000 || stream.rateUp > 50_000) && (
          <Zap size={12} className="shrink-0 text-accent" />
        )}
      </div>

      {/* Отправлено */}
      <div className="w-24 text-right">
        <div className="font-mono text-xs text-fg">
          {formatBytes(stream.totalUp)}
        </div>
        {/* Зарезервированная строка скорости — чтобы высота строки не прыгала
            при появлении/исчезновении ненулевой скорости. */}
        <div className="flex h-[14px] items-center justify-end gap-0.5 text-[10px] text-muted">
          {stream.active && stream.rateUp > 0 && (
            <>
              <ArrowUpRight size={9} />
              {formatRate(stream.rateUp)}
            </>
          )}
        </div>
      </div>

      {/* Получено */}
      <div className="w-24 text-right">
        <div className="font-mono text-xs text-fg">
          {formatBytes(stream.totalDown)}
        </div>
        <div className="flex h-[14px] items-center justify-end gap-0.5 text-[10px] text-muted">
          {stream.active && stream.rateDown > 0 && (
            <>
              <ArrowDownRight size={9} />
              {formatRate(stream.rateDown)}
            </>
          )}
        </div>
      </div>
    </li>
  );
}

/* ---------- Хелперы ---------- */

function SummaryCard({
  icon,
  label,
  value,
  tone,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  tone?: "accent";
}) {
  return (
    <Card className="py-4">
      <div className="flex items-center gap-2 text-muted">
        <span className={tone === "accent" ? "text-accent" : ""}>{icon}</span>
        <span className="text-xs">{label}</span>
      </div>
      <div className="mt-2 font-mono text-xl font-semibold text-fg">{value}</div>
    </Card>
  );
}

function EmptyState() {
  return (
    <div className="flex h-32 flex-col items-center justify-center text-sm text-muted/70">
      <Activity size={28} className="mb-2" />
      Подключитесь к туннелю, чтобы видеть активные стримы
    </div>
  );
}
