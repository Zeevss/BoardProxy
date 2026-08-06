import * as React from "react";
import { api, Unauthorized } from "@/lib/api";

// Состояние авторизации: null — ещё проверяем (probe при загрузке), true/false —
// известно. На любой Unauthorized из API падаем в false → RequireAuth уводит на
// экран логина.

interface AuthState {
  authed: boolean | null;
  login: (password: string) => Promise<void>;
  logout: () => Promise<void>;
  markUnauthorized: () => void;
}

const AuthContext = React.createContext<AuthState | null>(null);

// registerUnauthorizedHandler позволяет слою запросов (QueryCache) сообщить о
// протухшей сессии без прямой зависимости на React-контекст.
let unauthorizedHandler: (() => void) | null = null;
export function notifyUnauthorized() {
  unauthorizedHandler?.();
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [authed, setAuthed] = React.useState<boolean | null>(null);

  React.useEffect(() => {
    unauthorizedHandler = () => setAuthed(false);
    return () => {
      unauthorizedHandler = null;
    };
  }, []);

  // Session belongs to the standalone panel, not to any selected node.
  React.useEffect(() => {
    let alive = true;
    api
      .session()
      .then(() => alive && setAuthed(true))
      .catch((e) => {
        if (!alive) return;
        setAuthed(e instanceof Unauthorized ? false : true);
      });
    return () => {
      alive = false;
    };
  }, []);

  const value: AuthState = {
    authed,
    login: async (password) => {
      await api.login(password);
      setAuthed(true);
    },
    logout: async () => {
      try {
        await api.logout();
      } finally {
        setAuthed(false);
      }
    },
    markUnauthorized: () => setAuthed(false),
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = React.useContext(AuthContext);
  if (!ctx) throw new Error("useAuth вне AuthProvider");
  return ctx;
}
