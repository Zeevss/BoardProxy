import { Navigate } from "react-router-dom";
import { Loader2 } from "lucide-react";
import { useAuth } from "@/lib/auth";

// RequireAuth пускает дальше только при известной валидной сессии. Пока идёт
// проба (authed === null) — показываем спиннер; при отсутствии сессии — редирект
// на /login.
export function RequireAuth({ children }: { children: React.ReactNode }) {
  const { authed } = useAuth();

  if (authed === null) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (!authed) return <Navigate to="/login" replace />;
  return <>{children}</>;
}
