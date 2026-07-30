import { Sun, Moon, Monitor } from "lucide-react";
import { cn } from "@/lib/utils";
import { useSettingsStore } from "@/store/settings";
import type { Theme } from "@/types";

const TITLES: Record<string, { title: string; subtitle: string }> = {
  dashboard: { title: "Обзор", subtitle: "Состояние туннеля и трафик" },
  profiles: { title: "Профили", subtitle: "Управление серверами подключений" },
  stats: { title: "Статистика", subtitle: "Активные TCP-стримы через туннель" },
  proxy: { title: "Настройки прокси", subtitle: "Локальный прокси и список обхода" },
  debug: { title: "Debug-графики", subtitle: "Регуляторы и состояние физических lanes" },
  logs: { title: "Логи", subtitle: "Поток событий туннеля" },
};

export function TopBar({ screen }: { screen: string }) {
  const { theme, followSystemTheme, setTheme, setFollowSystemTheme } =
    useSettingsStore();
  const meta = TITLES[screen] ?? { title: "BoardProxy", subtitle: "" };

  return (
    <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3 min-[900px]:px-8 min-[900px]:py-4">
      <div className="min-w-0">
        <h1 className="text-lg font-semibold text-fg">{meta.title}</h1>
        {meta.subtitle && (
          <p className="truncate text-xs text-muted">{meta.subtitle}</p>
        )}
      </div>

      {/* Переключатель темы */}
      <div className="inline-flex items-center gap-1 rounded-xl border border-border bg-surface-2 p-1">
        <ThemeButton
          active={!followSystemTheme && theme === "light"}
          onClick={() => setTheme("light")}
          label="Светлая"
        >
          <Sun size={15} />
        </ThemeButton>
        <ThemeButton
          active={!followSystemTheme && theme === "dark"}
          onClick={() => setTheme("dark")}
          label="Тёмная"
        >
          <Moon size={15} />
        </ThemeButton>
        <ThemeButton
          active={followSystemTheme}
          onClick={() => setFollowSystemTheme(true)}
          label="Системная"
        >
          <Monitor size={15} />
        </ThemeButton>
      </div>
    </header>
  );
}

function ThemeButton({
  active,
  onClick,
  label,
  children,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={cn(
        "flex h-7 w-8 items-center justify-center rounded-lg transition-colors",
        active
          ? "bg-accent text-accent-fg"
          : "text-muted hover:text-fg"
      )}
    >
      {children}
    </button>
  );
}
