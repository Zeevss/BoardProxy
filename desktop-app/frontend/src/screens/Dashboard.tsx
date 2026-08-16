import { useEffect, useState } from "react";
import {
  Shield,
  ArrowUpRight,
  ArrowDownRight,
  Timer,
  Gauge,
  ChevronRight,
  Plus,
  Globe,
  Network,
} from "lucide-react";
import { Card, CardHeader, CardTitle, LineChart, StatusDot } from "@/components/ui";
import { Button } from "@/components/ui";
import type { ReactNode } from "react";
import { useTunnelStore } from "@/store/tunnel";
import { useProfilesStore } from "@/store/profiles";
import { useSettingsStore } from "@/store/settings";
import { cn, formatBytes, formatDuration, formatRate } from "@/lib/utils";
import { linkSummary } from "@/lib/link";
import type { Screen } from "@/components/layout/Sidebar";

export function Dashboard({ onNavigate }: { onNavigate: (s: Screen) => void }) {
  const status = useTunnelStore((s) => s.status);
  const toggle = useTunnelStore((s) => s.toggle);
  const reconnect = useTunnelStore((s) => s.reconnect);
  const applySystemProxy = useTunnelStore((s) => s.applySystemProxy);
  const latency = useTunnelStore((s) => s.latency);
  const connectedAt = useTunnelStore((s) => s.connectedAt);
  const totalUp = useTunnelStore((s) => s.totalUp);
  const totalDown = useTunnelStore((s) => s.totalDown);
  const traffic = useTunnelStore((s) => s.traffic);

  const activeProfile = useProfilesStore((s) => s.getById(s.activeId));
  const hasProfiles = useProfilesStore((s) => s.profiles.length > 0);
  const port = useSettingsStore((s) => s.port);
  const systemProxy = useSettingsStore((s) => s.systemProxy);
  const tunMode = useSettingsStore((s) => s.tunMode);
  const toggleSystemProxy = useSettingsStore((s) => s.toggleSystemProxy);
  const toggleTunMode = useSettingsStore((s) => s.toggleTunMode);

  // Тикающий аптайм.
  const [, setTick] = useState(0);
  useEffect(() => {
    if (status !== "connected") return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [status]);

  const isOn = status === "connected";
  const isBusy =
    status === "connecting" ||
    status === "reconnecting" ||
    status === "stopping";
  const isConnecting = status === "connecting" || status === "reconnecting";
  const uptime = connectedAt && status === "connected" ? Date.now() - connectedAt : 0;

  const lastRate = traffic[traffic.length - 1];
  const downSeries = traffic.map((p) => p.down);
  const upSeries = traffic.map((p) => p.up);

  return (
    <div className="mx-auto max-w-4xl space-y-5">
      {/* Большая кнопка подключения */}
      <Card className="flex flex-col items-center gap-5 py-8">
        <div className="relative">
          {isConnecting && (
            <>
              <span className="absolute inset-0 -m-2 rounded-full border border-accent/40 bg-accent/10 animate-pulse-ring" />
              <span className="absolute inset-0 -m-2 rounded-full border border-accent/25 bg-accent/5 animate-pulse-ring [animation-delay:600ms]" />
            </>
          )}
          <button
            onClick={toggle}
            className={`relative flex h-28 w-28 items-center justify-center rounded-full transition-all duration-300 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-accent/30 ${
              isOn
                ? "border border-ok/40 bg-ok/15 text-ok shadow-[0_0_28px_rgb(var(--c-ok)/0.2)]"
                : isConnecting
                  ? "border border-accent/50 bg-accent/10 text-accent shadow-glow"
                  : status === "error"
                    ? "border border-danger/40 bg-danger/10 text-danger"
                    : "bg-surface-2 text-muted border border-border hover:text-fg hover:border-accent/50"
            } ${isBusy ? "opacity-70" : ""}`}
            aria-label={isOn ? "Отключить" : isBusy ? "Остановить" : "Подключиться"}
          >
            <Shield size={46} strokeWidth={2.05} />
          </button>
        </div>

        <div className="text-center">
          <div className="flex items-center justify-center gap-2">
            <StatusDot
              tone={isOn ? "ok" : isBusy ? "warn" : "muted"}
              pulse={isOn || isBusy}
            />
            <span className="text-base font-semibold text-fg">
              {isOn
                ? "Подключено"
                : status === "reconnecting"
                  ? "Переподключение…"
                  : status === "stopping"
                    ? "Остановка…"
                    : isBusy
                      ? "Подключение…"
                      : "Отключено"}
            </span>
          </div>
          <p className="mt-1 text-xs text-muted">
            {isOn
              ? "Туннель активен"
              : isBusy
              ? status === "reconnecting"
                ? "Соединение потеряно, BoardProxy восстанавливает сессию"
                : status === "stopping"
                  ? "Корректно завершаем активные соединения"
                  : "Установка соединения — нажмите, чтобы отменить"
              : "Нажмите кнопку, чтобы подключиться"}
          </p>
        </div>

        {/* Компактные переключатели режима: SOCKS поднимается всегда, TUN и
            системный прокси — независимые опции, применяются на лету. */}
        <div className="flex flex-wrap items-center justify-center gap-2">
          <PillToggle
            icon={<Globe size={13} />}
            label="Системный прокси"
            active={systemProxy}
            onClick={() => {
              const v = !systemProxy;
              toggleSystemProxy(v);
              applySystemProxy(v);
            }}
            disabled={isBusy}
          />
          <PillToggle
            icon={<Network size={13} />}
            label="TUN"
            active={tunMode}
            onClick={() => {
              toggleTunMode(!tunMode);
              reconnect();
            }}
            disabled={isBusy}
          />
        </div>
      </Card>

      {/* Активный профиль */}
      <Card>
        <CardHeader>
          <CardTitle>Активный профиль</CardTitle>
          <Button variant="ghost" size="sm" onClick={() => onNavigate("profiles")}>
            Управление <ChevronRight size={14} />
          </Button>
        </CardHeader>
        {activeProfile ? (
          <div className="flex flex-col items-start justify-between gap-3 min-[760px]:flex-row min-[760px]:items-center">
            <div className="min-w-0">
              <div className="text-sm font-medium text-fg">{activeProfile.name}</div>
              <div className="mt-0.5 truncate font-mono text-xs text-muted">
                {linkSummary(activeProfile.key)}
              </div>
            </div>
            <div className="flex max-w-full flex-wrap items-center gap-2 text-xs text-muted">
              <span className="max-w-full break-all rounded-lg bg-surface-2 px-2 py-1 font-mono">
                socks5://127.0.0.1:{port}
              </span>
              {systemProxy && (
                <span className="rounded-lg bg-accent/10 px-2 py-1 text-accent">
                  системный
                </span>
              )}
            </div>
          </div>
        ) : hasProfiles ? (
          <p className="text-sm text-muted">
            Профиль не выбран. Выберите его на вкладке «Профили».
          </p>
        ) : (
          <div className="flex flex-col items-center gap-3 py-2 text-center">
            <p className="text-sm text-muted">
              Нет ни одного профиля. Создайте первый, чтобы подключиться.
            </p>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => onNavigate("profiles")}
            >
              <Plus size={15} /> Добавить профиль
            </Button>
          </div>
        )}
      </Card>

      {/* Метрики */}
      <div className="grid grid-cols-1 gap-3 min-[620px]:grid-cols-2 min-[900px]:gap-4">
        <MetricCard
          icon={<Gauge size={16} />}
          label="Задержка"
          value={isOn ? `${latency} ms` : "—"}
          tone="accent"
        />
        <MetricCard
          icon={<Timer size={16} />}
          label="Время сессии"
          value={isOn ? formatDuration(uptime) : "—"}
          tone="muted"
        />
      </div>

      {/* Трафик */}
      <Card>
        <CardHeader>
          <CardTitle>Трафик</CardTitle>
          {isOn && lastRate && (
            <div className="flex items-center gap-3 text-xs">
              <span className="flex items-center gap-1 text-muted">
                <ArrowUpRight size={13} className="text-accent" />
                {formatRate(lastRate.up)}
              </span>
              <span className="flex items-center gap-1 text-muted">
                <ArrowDownRight size={13} className="text-accent" />
                {formatRate(lastRate.down)}
              </span>
            </div>
          )}
        </CardHeader>

        <div className="mb-3 grid grid-cols-1 gap-3 min-[620px]:grid-cols-2 min-[900px]:gap-4">
          <TrafficStat label="Отправлено" total={totalUp} series={upSeries} />
          <TrafficStat label="Получено" total={totalDown} series={downSeries} />
        </div>

        {isOn ? (
          <LineChart
            height={112}
            series={[
              { label: "Upload", color: "#f59e0b", values: upSeries },
              { label: "Download", color: "#3b82f6", values: downSeries },
            ]}
            formatValue={formatRate}
          />
        ) : (
          <div className="flex h-16 items-center justify-center text-xs text-muted/60">
            Подключитесь, чтобы видеть трафик
          </div>
        )}
      </Card>
    </div>
  );
}

function MetricCard({
  icon,
  label,
  value,
  tone,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  tone: "accent" | "muted";
}) {
  return (
    <Card className="py-4">
      <div className="flex items-center gap-2 text-muted">
        <span className={tone === "accent" ? "text-accent" : ""}>{icon}</span>
        <span className="text-xs">{label}</span>
      </div>
      <div className="mt-2 font-mono text-2xl font-semibold text-fg">{value}</div>
    </Card>
  );
}

function TrafficStat({
  label,
  total,
  series,
}: {
  label: string;
  total: number;
  series: number[];
}) {
  const current = series[series.length - 1] ?? 0;
  return (
    <div>
      <div className="label">{label}</div>
      <div className="mt-1 flex items-baseline gap-2">
        <span className="font-mono text-lg font-semibold text-fg">
          {formatBytes(total)}
        </span>
        <span className="text-[11px] text-muted">{formatRate(current)}</span>
      </div>
    </div>
  );
}

/** Компактная пилюля-переключатель режима. */
function PillToggle({
  icon,
  label,
  active,
  onClick,
  disabled,
}: {
  icon: ReactNode;
  label: string;
  active: boolean;
  onClick: () => void;
  disabled: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      className={cn(
        "flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
        active
          ? "border-accent bg-accent/10 text-accent"
          : "border-border text-muted hover:border-accent/40 hover:text-fg",
        disabled && "cursor-not-allowed opacity-50 hover:border-border hover:text-muted"
      )}
    >
      {icon}
      {label}
    </button>
  );
}
