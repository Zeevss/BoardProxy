import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  LayoutDashboard,
  Users,
  LayoutPanelTop,
  ScrollText,
  Wrench,
  LogOut,
  RefreshCw,
  Network,
} from "lucide-react";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";

const NAV = [
  { to: "/", label: "Обзор", icon: LayoutDashboard, end: true },
  { to: "/clients", label: "Клиенты", icon: Users, end: false },
  { to: "/boards", label: "Доски", icon: LayoutPanelTop, end: false },
  { to: "/logs", label: "Логи", icon: ScrollText, end: false },
  { to: "/maintenance", label: "Обслуживание", icon: Wrench, end: false },
];

export function AppLayout() {
  const { logout } = useAuth();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const restart = useMutation({
    mutationFn: () => api.restart(),
    onSuccess: () => {
      toast.success("Сервер перезапускается", {
        description: "Данные обновятся через несколько секунд.",
      });
      setTimeout(() => qc.invalidateQueries(), 3000);
    },
    onError: (e: Error) => toast.error("Не удалось перезапустить", { description: e.message }),
  });

  return (
    <div className="flex min-h-screen bg-background">
      {/* Сайдбар */}
      <aside className="hidden w-60 shrink-0 flex-col border-r bg-card/40 p-4 md:flex">
        <div className="mb-6 flex items-center gap-2 px-2">
          <Network className="h-5 w-5 text-primary" />
          <div className="leading-tight">
            <div className="text-sm font-semibold">BoardProxy</div>
            <div className="text-xs text-muted-foreground">Админ-панель</div>
          </div>
        </div>
        <nav className="flex flex-col gap-1">
          {NAV.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                )
              }
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>

      {/* Основная область */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between gap-4 border-b px-4 md:px-6">
          {/* Мобильная навигация */}
          <nav className="flex items-center gap-1 overflow-x-auto md:hidden">
            {NAV.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cn(
                    "rounded-md px-2.5 py-1.5 text-xs font-medium",
                    isActive ? "bg-primary text-primary-foreground" : "text-muted-foreground"
                  )
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
          <div className="hidden md:block" />

          <div className="flex items-center gap-2">
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button variant="outline" size="sm">
                  <RefreshCw className="h-4 w-4" />
                  <span className="hidden sm:inline">Перезапуск</span>
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Перезапустить сервер?</AlertDialogTitle>
                  <AlertDialogDescription>
                    Активные подключения клиентов будут разорваны, сервер заново
                    подключится к доске. Обычно занимает несколько секунд.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Отмена</AlertDialogCancel>
                  <AlertDialogAction onClick={() => restart.mutate()}>
                    Перезапустить
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>

            <Button
              variant="ghost"
              size="sm"
              onClick={async () => {
                await logout();
                navigate("/login", { replace: true });
              }}
            >
              <LogOut className="h-4 w-4" />
              <span className="hidden sm:inline">Выйти</span>
            </Button>
          </div>
        </header>

        <main className="flex-1 overflow-auto p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
