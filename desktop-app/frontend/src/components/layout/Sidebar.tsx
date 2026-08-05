import {
  LayoutDashboard,
  Users,
  Settings2,
  ScrollText,
  Shield,
  Activity,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { StatusDot } from "@/components/ui";
import { useTunnelStore } from "@/store/tunnel";
import type { TunnelStatus } from "@/types";

export type Screen = "dashboard" | "profiles" | "stats" | "proxy" | "debug" | "logs";

const NAV: Array<{ id: Screen; label: string; icon: typeof LayoutDashboard }> = [
  { id: "dashboard", label: "Обзор", icon: LayoutDashboard },
  { id: "profiles", label: "Профили", icon: Users },
  { id: "stats", label: "Статистика", icon: Activity },
  { id: "proxy", label: "Настройки прокси", icon: Settings2 },
  { id: "logs", label: "Логи", icon: ScrollText },
];

const STATUS_META: Record<
  TunnelStatus,
  { label: string; tone: "ok" | "warn" | "danger" | "muted"; pulse: boolean }
> = {
  connected: { label: "Подключено", tone: "ok", pulse: true },
  connecting: { label: "Подключение…", tone: "warn", pulse: true },
  reconnecting: { label: "Переподключение…", tone: "warn", pulse: true },
  stopping: { label: "Остановка…", tone: "warn", pulse: true },
  disconnected: { label: "Отключено", tone: "muted", pulse: false },
  error: { label: "Ошибка", tone: "danger", pulse: true },
};

export function Sidebar({
  active,
  onNavigate,
}: {
  active: Screen;
  onNavigate: (s: Screen) => void;
}) {
  const status = useTunnelStore((s) => s.status);
	const disconnect = useTunnelStore((s) => s.disconnect);
  const meta = STATUS_META[status];

  return (
    <aside className="flex h-full w-16 shrink-0 flex-col border-r border-border bg-surface transition-[width] min-[900px]:w-60">
      {/* Лого / бренд */}
      <div className="flex items-center justify-center gap-2.5 px-3 py-5 min-[900px]:justify-start min-[900px]:px-5">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent/15 text-accent">
          <Shield size={20} />
        </div>
        <div className="hidden leading-tight overflow-hidden min-[900px]:block">
          <div className="text-sm font-semibold text-fg">BoardProxy</div>
          <div className="text-[11px] text-muted">v0.1.0 · beta</div>
        </div>
      </div>

      {/* Навигация */}
      <nav className="flex-1 space-y-1 px-2 py-2 min-[900px]:px-3">
        {NAV.map(({ id, label, icon: Icon }) => {
          const isActive = id === active;
          return (
            <button
              key={id}
              onClick={() => onNavigate(id)}
              className={cn(
                "flex w-full items-center justify-center gap-3 rounded-xl px-2 py-2 text-sm font-medium transition-colors min-[900px]:justify-start min-[900px]:px-3",
                isActive
                  ? "bg-accent/10 text-accent"
                  : "text-muted hover:bg-surface-2 hover:text-fg"
              )}
            >
              <Icon size={18} className="shrink-0" />
              <span className="hidden min-[900px]:inline">{label}</span>
            </button>
          );
        })}
      </nav>

      {/* Статус внизу */}
      <div className="border-t border-border px-3 py-4 min-[900px]:px-5">
		<button type="button"
		  onClick={() => status !== "disconnected" && void disconnect()}
		  disabled={status === "disconnected"}
		  className="flex w-full items-center justify-center gap-2.5 rounded-lg py-1 text-left disabled:cursor-default min-[900px]:justify-start"
		  title={status === "disconnected" ? undefined : "Остановить и откатить подключение"}>
          <StatusDot tone={meta.tone} pulse={meta.pulse} />
          <span className="hidden text-xs font-medium text-fg min-[900px]:inline">{meta.label}</span>
		</button>
      </div>
    </aside>
  );
}
