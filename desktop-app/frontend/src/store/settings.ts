import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { ProxySettings, Theme } from "@/types";
import { applyTheme, getSystemTheme } from "@/lib/theme";
import { backend } from "@/lib/backend";

interface SettingsState extends ProxySettings {
  theme: Theme;
  /** Следовать системной теме. */
  followSystemTheme: boolean;

  setPort: (port: number) => void;
  setListenAddr: (addr: string) => void;
  setMaxLanes: (maxLanes: number) => void;
  toggleSystemProxy: (v?: boolean) => void;
  toggleTunMode: (v?: boolean) => void;
  addBypass: (domain: string) => void;
  removeBypass: (domain: string) => void;
  setBypassList: (list: string[]) => void;

  setTheme: (theme: Theme) => void;
  setFollowSystemTheme: (v: boolean) => void;
  /** Применяет текущую тему к DOM. */
  syncThemeToDom: () => void;
}

const DEFAULTS = {
  port: 1080,
  listenAddr: "127.0.0.1",
  systemProxy: true,
  tunMode: false,
  maxLanes: 4,
  // Паттерны bypass — регулярные выражения (RE2, как на Go-стороне), матчатся
  // по имени хоста без порта.
  bypassList: ["^localhost$", "^127\\.0\\.0\\.1$", "^::1$", "\\.local$"],
  theme: "dark" as Theme,
  followSystemTheme: true,
};

function resolveTheme(s: { theme: Theme; followSystemTheme: boolean }): Theme {
  return s.followSystemTheme ? getSystemTheme() : s.theme;
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set, get) => ({
      ...DEFAULTS,

      setPort: (port) => set({ port }),
      setListenAddr: (listenAddr) => set({ listenAddr }),
      setMaxLanes: (maxLanes) => set({ maxLanes }),
      toggleSystemProxy: (v) =>
        set((s) => ({ systemProxy: v ?? !s.systemProxy })),
      toggleTunMode: (v) => set((s) => ({ tunMode: v ?? !s.tunMode })),
      addBypass: (domain) => {
        const cur = get().bypassList;
        if (cur.includes(domain)) return;
        const bypassList = [...cur, domain];
        set({ bypassList });
        backend.updateBypassList(bypassList);
      },
      removeBypass: (domain) => {
        const bypassList = get().bypassList.filter((d) => d !== domain);
        set({ bypassList });
        backend.updateBypassList(bypassList);
      },
      setBypassList: (list) => {
        set({ bypassList: list });
        backend.updateBypassList(list);
      },

      setTheme: (theme) => {
        set({ theme, followSystemTheme: false });
        get().syncThemeToDom();
      },
      setFollowSystemTheme: (followSystemTheme) => {
        set({ followSystemTheme });
        get().syncThemeToDom();
      },
      syncThemeToDom: () => applyTheme(resolveTheme(get())),
    }),
    {
      name: "boardproxy.settings",
      version: 1,
      partialize: (s) => ({
        port: s.port,
        listenAddr: s.listenAddr,
        systemProxy: s.systemProxy,
        tunMode: s.tunMode,
        maxLanes: s.maxLanes,
        bypassList: s.bypassList,
        theme: s.theme,
        followSystemTheme: s.followSystemTheme,
      }),
    }
  )
);
