import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  Activity,
  ArrowDownToLine,
  ArrowUpFromLine,
  Cable,
  DatabaseZap,
  RefreshCw,
} from "lucide-react";
import { api, type ServerStats } from "@/lib/api";
import { formatBytes, formatDate, formatDuration, formatRate } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const C = {
  rawRx: "#e8733a",
  rawTx: "#cf4d7f",
  payloadRx: "#3987e5",
  payloadTx: "#199e70",
  axis: "#898781",
  grid: "#2c2c2a",
  surface: "#1a1a19",
};

type TrafficPoint = {
  key: string;
  time: string;
  rawRx: number;
  rawTx: number;
  payloadRx: number;
  payloadTx: number;
};

type PreviousSample = {
  sampledAt: number;
  rxBytes: number;
  txBytes: number;
};

export function Statistics() {
  const { data, isLoading } = useQuery({
    queryKey: ["stats"],
    queryFn: () => api.stats(),
    refetchInterval: 5000,
  });
  const [history, setHistory] = React.useState<TrafficPoint[]>([]);
  const previous = React.useRef<PreviousSample | null>(null);

  React.useEffect(() => {
    if (!data?.network?.available) return;
    const sampledAt = Date.parse(data.network.sampled_at);
    if (!Number.isFinite(sampledAt) || previous.current?.sampledAt === sampledAt) return;

    let payloadRx = 0;
    let payloadTx = 0;
    const before = previous.current;
    if (before && sampledAt > before.sampledAt) {
      const seconds = (sampledAt - before.sampledAt) / 1000;
      if (data.rx_bytes >= before.rxBytes) payloadRx = (data.rx_bytes - before.rxBytes) / seconds;
      if (data.tx_bytes >= before.txBytes) payloadTx = (data.tx_bytes - before.txBytes) / seconds;
    }
    const point: TrafficPoint = {
      key: data.network.sampled_at,
      time: new Date(sampledAt).toLocaleTimeString("ru-RU", {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      }),
      rawRx: data.network.rx_bytes_per_second,
      rawTx: data.network.tx_bytes_per_second,
      payloadRx,
      payloadTx,
    };
    previous.current = { sampledAt, rxBytes: data.rx_bytes, txBytes: data.tx_bytes };
    setHistory((points) => [...points, point].slice(-120));
  }, [data]);

  const last = history.at(-1);
  const rawRate = (last?.rawRx ?? 0) + (last?.rawTx ?? 0);
  const payloadRate = (last?.payloadRx ?? 0) + (last?.payloadTx ?? 0);
  const overhead = payloadRate > 0 ? rawRate / payloadRate : null;
  const reconnecting = (data?.transport.reconnects_last_minute ?? 0) > 0;

  return (
    <div className="mx-auto max-w-7xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Статистика</h1>
        <p className="text-sm text-muted-foreground">
          Raw-сеть core, полезный proxy-трафик и состояние транспорта
        </p>
      </div>

      <Card>
        <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
          <div>
            <CardTitle className="text-base">Источник raw-метрик</CardTitle>
            <p className="mt-1 text-xs text-muted-foreground">
              {data?.network.scope === "host_bridge"
                ? "Счётчики всего host bridge: core, panel, WebSocket, snapshots и служебный трафик."
                : "Счётчики default-route интерфейса в network namespace core; сюда входят snapshots, WebSocket и служебный трафик."}
            </p>
          </div>
          {data?.network.available ? <Badge variant="success">доступен</Badge> : <Badge variant="muted">нет данных</Badge>}
        </CardHeader>
        <CardContent className="flex flex-wrap gap-x-6 gap-y-2 text-xs text-muted-foreground">
          <span>Интерфейс: <strong className="text-foreground">{data?.network.interfaces?.join(", ") || "—"}</strong></span>
          <span>Scope: <strong className="text-foreground">{data?.network.scope || "—"}</strong></span>
          <span>Мониторинг с: <strong className="text-foreground">{data?.network.available ? formatDate(data.network.started_at) : "—"}</strong></span>
        </CardContent>
      </Card>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-3 xl:grid-cols-6">
        <Metric label="Raw RX" value={data ? formatRate(data.network.rx_bytes_per_second) : undefined}
          hint={data ? `${formatBytes(data.network.rx_bytes_since_start)} с запуска` : undefined}
          loading={isLoading} icon={ArrowDownToLine} />
        <Metric label="Raw TX" value={data ? formatRate(data.network.tx_bytes_per_second) : undefined}
          hint={data ? `${formatBytes(data.network.tx_bytes_since_start)} с запуска` : undefined}
          loading={isLoading} icon={ArrowUpFromLine} />
        <Metric label="Payload upload" value={last ? formatRate(last.payloadRx) : "накапливается"}
          hint={data ? `${formatBytes(data.rx_bytes)} всего` : undefined}
          loading={isLoading} icon={Cable} />
        <Metric label="Payload download" value={last ? formatRate(last.payloadTx) : "накапливается"}
          hint={data ? `${formatBytes(data.tx_bytes)} всего` : undefined}
          loading={isLoading} icon={Cable} />
        <Metric label="Raw / payload" value={overhead === null ? "—" : `${overhead.toFixed(2)}×`}
          hint="Текущая доля overhead" loading={isLoading} icon={Activity} />
        <Metric label="Reconnect за минуту" value={data?.transport.reconnects_last_minute}
          hint={data ? `${formatBytes(data.transport.snapshot_bytes_last_minute)} snapshots` : undefined}
          loading={isLoading} icon={RefreshCw} alert={reconnecting} />
      </div>

      <TrafficChart data={history} loading={isLoading} />

      <div className="grid gap-4 xl:grid-cols-2">
        <RuntimeSummary stats={data} loading={isLoading} />
        <TransportSummary stats={data} loading={isLoading} />
      </div>

      <UserTraffic stats={data} loading={isLoading} />
      <TransportRoles stats={data} loading={isLoading} />
      <BoardTraffic stats={data} loading={isLoading} />
    </div>
  );
}

function TrafficChart({ data, loading }: { data: TrafficPoint[]; loading: boolean }) {
  const names: Record<string, string> = {
    rawRx: "Raw RX", rawTx: "Raw TX", payloadRx: "Payload upload", payloadTx: "Payload download",
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Скорость трафика</CardTitle>
        <p className="text-xs text-muted-foreground">Последние 10 минут в пределах открытой вкладки</p>
      </CardHeader>
      <CardContent>
        {loading ? <Skeleton className="h-72 w-full" /> : (
          <ResponsiveContainer width="100%" height={288}>
            <LineChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 12 }}>
              <CartesianGrid vertical={false} stroke={C.grid} />
              <XAxis dataKey="time" stroke={C.axis} tick={{ fontSize: 11, fill: C.axis }} tickLine={false} minTickGap={28} />
              <YAxis stroke={C.axis} tick={{ fontSize: 11, fill: C.axis }} tickLine={false}
                tickFormatter={(v) => formatRate(v)} width={76} domain={[0, "auto"]} />
              <Tooltip contentStyle={{ background: C.surface, border: `1px solid ${C.grid}`, borderRadius: 8, fontSize: 12 }}
                formatter={(v: number, name) => [formatRate(v), names[String(name)] ?? name]} />
              <Legend formatter={(v) => names[v] ?? v} wrapperStyle={{ fontSize: 12 }} />
              <Line type="monotone" dataKey="rawRx" stroke={C.rawRx} dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="rawTx" stroke={C.rawTx} dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="payloadRx" stroke={C.payloadRx} dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="payloadTx" stroke={C.payloadTx} dot={false} strokeWidth={2} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}

function RuntimeSummary({ stats, loading }: { stats?: ServerStats; loading: boolean }) {
  return (
    <Card>
      <CardHeader><CardTitle className="text-base">Живая нагрузка</CardTitle></CardHeader>
      <CardContent className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Small label="Пользователи" value={stats?.online_users} loading={loading} />
        <Small label="Соединения" value={stats?.active_connections} loading={loading} />
        <Small label="Lanes" value={stats?.active_lanes} loading={loading} />
        <Small label="Стримы" value={stats?.active_streams} loading={loading} />
      </CardContent>
    </Card>
  );
}

function TransportSummary({ stats, loading }: { stats?: ServerStats; loading: boolean }) {
  const t = stats?.transport;
  return (
    <Card>
      <CardHeader><CardTitle className="text-base">Reconnect и snapshots</CardTitle></CardHeader>
      <CardContent className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Small label="Reconnect всего" value={t?.reconnects_total} loading={loading} />
        <Small label="Неудачных попыток" value={t?.reconnect_attempts_failed} loading={loading} />
        <Small label="Circuit open" value={t?.circuit_open_total} loading={loading} />
        <Small label="Snapshot всего" value={t ? formatBytes(t.snapshot_bytes_total) : undefined} loading={loading} />
        <Small label="Последний downtime" value={t ? formatDuration(t.last_downtime_ms) : undefined} loading={loading} />
        <Small label="Очисток страниц" value={stats?.page_cleanup_runs} loading={loading} />
        <Small label="Удалено объектов" value={stats?.page_cleanup_deleted} loading={loading} />
        <Small label="Карантин страниц" value={stats?.page_cleanup_quarantined} loading={loading} />
      </CardContent>
      {t?.last_disconnect_reason && (
        <div className="border-t px-6 py-3 text-xs text-muted-foreground">
          Последний обрыв: <span className="break-all font-mono text-foreground">{t.last_disconnect_reason}</span>
        </div>
      )}
    </Card>
  );
}

function UserTraffic({ stats, loading }: { stats?: ServerStats; loading: boolean }) {
  const rows = [...(stats?.users ?? [])].sort((a, b) => Number(b.online) - Number(a.online) || (b.rx_bytes + b.tx_bytes) - (a.rx_bytes + a.tx_bytes));
  return (
    <Card>
      <CardHeader><CardTitle className="text-base">Трафик по пользователям</CardTitle></CardHeader>
      <Table>
        <TableHeader><TableRow>
          <TableHead>Пользователь</TableHead><TableHead>Состояние</TableHead>
          <TableHead>Conn / lanes / streams</TableHead><TableHead>Upload</TableHead><TableHead>Download</TableHead><TableHead>Был на связи</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {loading ? <LoadingRow cols={6} /> : rows.length === 0 ? <EmptyRow cols={6} /> : rows.map((u) => (
            <TableRow key={u.id}>
              <TableCell><div className="font-medium">{u.name}</div><div className="font-mono text-xs text-muted-foreground">ID {u.id}</div></TableCell>
              <TableCell><Badge variant={u.online ? "success" : u.status === "disabled" ? "muted" : "outline"}>{u.online ? "онлайн" : u.status === "disabled" ? "отключён" : "офлайн"}</Badge></TableCell>
              <TableCell className="tabular-nums">{u.connections} / {u.lanes} / {u.streams}</TableCell>
              <TableCell className="tabular-nums">{formatBytes(u.rx_bytes)}{u.active_rx_bytes > 0 && <div className="text-xs text-muted-foreground">live {formatBytes(u.active_rx_bytes)}</div>}</TableCell>
              <TableCell className="tabular-nums">{formatBytes(u.tx_bytes)}{u.active_tx_bytes > 0 && <div className="text-xs text-muted-foreground">live {formatBytes(u.active_tx_bytes)}</div>}</TableCell>
              <TableCell className="text-muted-foreground">{formatDate(u.last_seen)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  );
}

function TransportRoles({ stats, loading }: { stats?: ServerStats; loading: boolean }) {
  const rows = stats?.transport.per_role ?? [];
  return (
    <Card>
      <CardHeader><CardTitle className="text-base">Reconnect по роли и доске</CardTitle></CardHeader>
      <Table>
        <TableHeader><TableRow>
          <TableHead>Доска / роль</TableHead><TableHead>Reconnect</TableHead><TableHead>За минуту</TableHead>
          <TableHead>Ошибки попыток</TableHead><TableHead>Snapshots</TableHead><TableHead>Последнее соединение</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {loading ? <LoadingRow cols={6} /> : rows.length === 0 ? <EmptyRow cols={6} text="Reconnect пока не было" /> : rows.map((r) => (
            <TableRow key={`${r.board}:${r.role}`}>
              <TableCell><div className="font-mono text-xs">{shortHash(r.board)}</div><div className="text-xs text-muted-foreground">{roleLabel(r.role)}</div></TableCell>
              <TableCell className="tabular-nums">{r.reconnects_total}</TableCell>
              <TableCell><Badge variant={r.reconnects_last_minute > 0 ? "destructive" : "muted"}>{r.reconnects_last_minute}</Badge></TableCell>
              <TableCell className="tabular-nums">{r.reconnect_attempts_failed}</TableCell>
              <TableCell className="tabular-nums">{formatBytes(r.snapshot_bytes_total)}<div className="text-xs text-muted-foreground">{r.snapshot_objects_total} объектов</div></TableCell>
              <TableCell>{formatDuration(r.last_connected_for_ms)}<div className="text-xs text-muted-foreground">downtime {formatDuration(r.last_downtime_ms)}</div></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  );
}

function BoardTraffic({ stats, loading }: { stats?: ServerStats; loading: boolean }) {
  const rows = stats?.per_board ?? [];
  return (
    <Card>
      <CardHeader><CardTitle className="text-base">Активный трафик по доскам</CardTitle></CardHeader>
      <Table>
        <TableHeader><TableRow><TableHead>Доска</TableHead><TableHead>Клиенты</TableHead><TableHead>Свободные страницы</TableHead><TableHead>Upload</TableHead><TableHead>Download</TableHead></TableRow></TableHeader>
        <TableBody>
          {loading ? <LoadingRow cols={5} /> : rows.length === 0 ? <EmptyRow cols={5} text="Нет поднятых хабов" /> : rows.map((b) => (
            <TableRow key={b.id}><TableCell><div className="font-medium">{b.name}</div><div className="font-mono text-xs text-muted-foreground">{shortHash(b.id)}</div></TableCell><TableCell>{b.clients_online}</TableCell><TableCell>{b.free_pages}</TableCell><TableCell>{formatBytes(b.rx_bytes)}</TableCell><TableCell>{formatBytes(b.tx_bytes)}</TableCell></TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  );
}

function Metric({ label, value, hint, loading, icon: Icon, alert = false }: { label: string; value?: number | string; hint?: string; loading: boolean; icon: React.ComponentType<{ className?: string }>; alert?: boolean }) {
  return <Card><CardHeader className="flex-row items-center justify-between space-y-0 pb-2"><CardTitle className="text-xs font-medium text-muted-foreground">{label}</CardTitle><Icon className={alert ? "h-4 w-4 text-destructive" : "h-4 w-4 text-muted-foreground"} /></CardHeader><CardContent>{loading ? <Skeleton className="h-7 w-20" /> : <div className="text-xl font-semibold tabular-nums">{value ?? "—"}</div>}{hint && !loading && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}</CardContent></Card>;
}

function Small({ label, value, loading }: { label: string; value?: number | string; loading: boolean }) {
  return <div>{loading ? <Skeleton className="h-7 w-14" /> : <div className="text-xl font-semibold tabular-nums">{value ?? "—"}</div>}<div className="mt-1 text-xs text-muted-foreground">{label}</div></div>;
}

function LoadingRow({ cols }: { cols: number }) {
  return <TableRow><TableCell colSpan={cols}><Skeleton className="h-10 w-full" /></TableCell></TableRow>;
}

function EmptyRow({ cols, text = "Нет данных" }: { cols: number; text?: string }) {
  return <TableRow><TableCell colSpan={cols} className="py-8 text-center text-muted-foreground"><DatabaseZap className="mx-auto mb-2 h-5 w-5" />{text}</TableCell></TableRow>;
}

function shortHash(value: string): string { return value.length > 18 ? `${value.slice(0, 16)}…` : value || "—"; }
function roleLabel(role: string): string { return role === "server-lane" ? "Канал клиента" : role === "hub-control" ? "Управляющий хаб" : role; }
