import { useEffect, useRef, useState } from "react";
import { Sidebar } from "@/components/layout/Sidebar";
import { TopBar } from "@/components/layout/TopBar";
import type { Screen } from "@/components/layout/Sidebar";
import { Dashboard } from "@/screens/Dashboard";
import { Profiles } from "@/screens/Profiles";
import { ProxySettings } from "@/screens/ProxySettings";
import { Logs } from "@/screens/Logs";
import { Stats } from "@/screens/Stats";
import { DebugCharts } from "@/screens/DebugCharts";
import { Welcome } from "@/screens/Welcome";
import { useSettingsStore } from "@/store/settings";
import { useProfilesStore } from "@/store/profiles";
import { useTunnelStore } from "@/store/tunnel";
import { backend } from "@/lib/backend";

function App() {
  const [screen, setScreen] = useState<Screen>("dashboard");
  const syncThemeToDom = useSettingsStore((s) => s.syncThemeToDom);
  const followSystemTheme = useSettingsStore((s) => s.followSystemTheme);
  const hasProfiles = useProfilesStore((s) => s.profiles.length > 0);

  const status = useTunnelStore((s) => s.status);
  const profiles = useProfilesStore((s) => s.profiles);
  const activeId = useProfilesStore((s) => s.activeId);

  // Применяем тему при старте и при смене системной темы.
  useEffect(() => {
    syncThemeToDom();
    if (!followSystemTheme) return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => syncThemeToDom();
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [syncThemeToDom, followSystemTheme]);

  // Действия из меню трея (один раз за загрузку).
  useEffect(() => {
    const offToggle = backend.onTrayToggle(() => useTunnelStore.getState().toggle());
    const offSelect = backend.onTraySelectProfile((id) =>
      useProfilesStore.getState().setActive(id)
    );
    return () => {
      offToggle();
      offSelect();
    };
  }, []);

  // Держим меню трея в актуальном состоянии.
  useEffect(() => {
    backend.syncTray({
      status,
      profiles: profiles.map((p) => ({ id: p.id, name: p.name })),
      activeId: activeId ?? "",
    });
  }, [status, profiles, activeId]);

  // Смена активного профиля во время подключения → переподключение на него.
  const prevActive = useRef(activeId);
  useEffect(() => {
    if (prevActive.current === activeId) return;
    prevActive.current = activeId;
    const st = useTunnelStore.getState().status;
    if (st === "connected" || st === "connecting" || st === "reconnecting") {
      useTunnelStore.getState().reconnect();
    }
  }, [activeId]);

  // Нет ни одного профиля — приветственный экран с вставкой ключа из буфера.
  if (!hasProfiles) {
    return <Welcome />;
  }

  return (
    <div className="flex h-full w-full">
      <Sidebar active={screen} onNavigate={setScreen} />
      <div className="flex h-full min-w-0 flex-1 flex-col overflow-hidden">
        <TopBar screen={screen} />
        <main className="flex-1 overflow-y-auto px-4 py-4 min-[900px]:px-8 min-[900px]:py-6">
          {screen === "dashboard" && <Dashboard onNavigate={setScreen} />}
          {screen === "profiles" && <Profiles />}
          {screen === "stats" && <Stats />}
          {screen === "proxy" && <ProxySettings onOpenDebug={() => setScreen("debug")} />}
          {screen === "debug" && <DebugCharts />}
          {screen === "logs" && <Logs />}
        </main>
      </div>
    </div>
  );
}

export default App;
