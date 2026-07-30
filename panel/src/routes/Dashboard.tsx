import { useQuery } from "@tanstack/react-query";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  Users,
  UserCheck,
  Radio,
  LayoutPanelTop,
  ArrowDownToLine,
  ArrowUpFromLine,
  FileStack,
  Waypoints,
} from "lucide-react";
import { api, type ServerStats } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";

// Цвета серий (валидированы на тёмной поверхности панели): rx=upload синий,
// tx=download аква. Ось/сетка — приглушённые.
const C = {
  rx: "#3987e5",
  tx: "#199e70",
  clients: "#3987e5",
  axis: "#898781",
  grid: "#2c2c2a",
  surface: "#1a1a19",
};

export function Dashboard() {
  const { data, isLoading } = useQuery({
    queryKey: ["stats"],
    queryFn: () => api.stats(),
    refetchInterval: 5000,
  });

  const perBoard = data?.per_board ?? [];

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Обзор</h1>
        <p className="text-sm text-muted-foreground">Состояние сервера BoardProxy</p>
      </div>

      <HubStatus stats={data} loading={isLoading} />

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat label="Клиенты" value={data?.clients} loading={isLoading} icon={Users}
          hint={data ? `${data.clients_active} активных` : undefined} />
        <Stat label="Онлайн" value={data?.clients_online} loading={isLoading} icon={Radio} />
        <Stat label="Доски (активны)" value={data ? `${data.boards_active}/${data.boards}` : undefined}
          loading={isLoading} icon={LayoutPanelTop} hint={data ? `${data.hubs_up} обслуживается` : undefined} />
        <Stat label="Свободные страницы" value={data?.free_pages} loading={isLoading} icon={FileStack} />
        <Stat label="Получено (upload)" value={data ? formatBytes(data.rx_bytes) : undefined}
          loading={isLoading} icon={ArrowUpFromLine} />
        <Stat label="Отправлено (download)" value={data ? formatBytes(data.tx_bytes) : undefined}
          loading={isLoading} icon={ArrowDownToLine} />
        <Stat label="Клиентов активно" value={data?.clients_active} loading={isLoading} icon={UserCheck} />
        <Stat label="Хабов поднято" value={data?.hubs_up} loading={isLoading} icon={Waypoints} />
      </div>

      {/* Разбивка по доскам — только когда поднято больше одного хаба, иначе
          график не добавляет информации к карточкам-сводам. */}
      {perBoard.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          <TrafficByBoard data={perBoard} loading={isLoading} />
          <ClientsByBoard data={perBoard} loading={isLoading} />
        </div>
      )}
    </div>
  );
}

// boardLabel — короткая подпись доски по оси: имя либо усечённый хэш.
function boardLabel(b: { id: string; name: string }): string {
  if (b.name && b.name !== b.id) return b.name;
  return b.id.length > 10 ? b.id.slice(0, 8) + "…" : b.id;
}

function TrafficByBoard({
  data,
  loading,
}: {
  data: ServerStats["per_board"];
  loading: boolean;
}) {
  const rows = (data ?? []).map((b) => ({
    board: boardLabel(b),
    rx: b.rx_bytes,
    tx: b.tx_bytes,
  }));
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Трафик по доскам</CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-56 w-full" />
        ) : (
          <ResponsiveContainer width="100%" height={224}>
            <BarChart data={rows} barGap={2} margin={{ top: 8, right: 8, bottom: 0, left: 8 }}>
              <CartesianGrid vertical={false} stroke={C.grid} />
              <XAxis dataKey="board" stroke={C.axis} tick={{ fontSize: 11, fill: C.axis }} tickLine={false} />
              <YAxis
                stroke={C.axis}
                tick={{ fontSize: 11, fill: C.axis }}
                tickLine={false}
                width={54}
                tickFormatter={(v) => formatBytes(v)}
              />
              <Tooltip
                cursor={{ fill: "rgba(255,255,255,0.05)" }}
                contentStyle={{ background: C.surface, border: `1px solid ${C.grid}`, borderRadius: 8, fontSize: 12 }}
                formatter={(v: number, n) => [formatBytes(v), n === "rx" ? "Получено" : "Отправлено"]}
              />
              <Legend
                formatter={(v) => (v === "rx" ? "Получено (upload)" : "Отправлено (download)")}
                wrapperStyle={{ fontSize: 12 }}
              />
              <Bar dataKey="rx" fill={C.rx} radius={[4, 4, 0, 0]} maxBarSize={40} />
              <Bar dataKey="tx" fill={C.tx} radius={[4, 4, 0, 0]} maxBarSize={40} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}

function ClientsByBoard({
  data,
  loading,
}: {
  data: ServerStats["per_board"];
  loading: boolean;
}) {
  const rows = (data ?? []).map((b) => ({ board: boardLabel(b), online: b.clients_online }));
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Клиенты онлайн по доскам</CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-56 w-full" />
        ) : (
          <ResponsiveContainer width="100%" height={224}>
            <BarChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 8 }}>
              <CartesianGrid vertical={false} stroke={C.grid} />
              <XAxis dataKey="board" stroke={C.axis} tick={{ fontSize: 11, fill: C.axis }} tickLine={false} />
              <YAxis
                stroke={C.axis}
                tick={{ fontSize: 11, fill: C.axis }}
                tickLine={false}
                width={28}
                allowDecimals={false}
              />
              <Tooltip
                cursor={{ fill: "rgba(255,255,255,0.05)" }}
                contentStyle={{ background: C.surface, border: `1px solid ${C.grid}`, borderRadius: 8, fontSize: 12 }}
                formatter={(v: number) => [v, "Онлайн"]}
              />
              <Bar dataKey="online" fill={C.clients} radius={[4, 4, 0, 0]} maxBarSize={40} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}

function HubStatus({ stats, loading }: { stats?: ServerStats; loading: boolean }) {
  const boards = stats?.serving_boards ?? [];
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-2">
          <Waypoints className="h-4 w-4 text-muted-foreground" />
          <CardTitle className="text-base">Хабы</CardTitle>
        </div>
        {loading ? (
          <Skeleton className="h-5 w-16" />
        ) : boards.length > 0 ? (
          <Badge variant="success">{boards.length} онлайн</Badge>
        ) : (
          <Badge variant="muted">Нет досок</Badge>
        )}
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-5 w-40" />
        ) : boards.length === 0 ? (
          <div className="text-sm text-muted-foreground">Доски не обслуживаются</div>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {boards.map((b) => (
              <span key={b} className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground">
                {b.length > 16 ? b.slice(0, 14) + "…" : b}
              </span>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Stat({
  label,
  value,
  hint,
  loading,
  icon: Icon,
}: {
  label: string;
  value?: number | string;
  hint?: string;
  loading: boolean;
  icon: React.ComponentType<{ className?: string }>;
}) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-xs font-medium text-muted-foreground">{label}</CardTitle>
        <Icon className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-8 w-20" />
        ) : (
          <div className="text-2xl font-semibold tabular-nums">{value ?? "—"}</div>
        )}
        {hint && !loading && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
      </CardContent>
    </Card>
  );
}
