import { Activity, Boxes, Gauge, Layers3 } from "lucide-react";
import { Card, CardHeader, CardTitle, LineChart, type LineSeries } from "@/components/ui";
import { formatBytes, formatRate } from "@/lib/utils";
import { useTunnelStore } from "@/store/tunnel";
import type { TransportDebugPoint } from "@/types";

const COLORS = ["#22c55e", "#3b82f6", "#f59e0b", "#ec4899"];

export function DebugCharts() {
  const points = useTunnelStore((state) => state.debug);
  const latest = points[points.length - 1];
  const laneIDs = Array.from(
    new Set(points.flatMap((point) => point.lanes.map((lane) => lane.id)))
  ).sort((a, b) => a - b);

  return (
    <div className="mx-auto max-w-5xl space-y-5">
      <div className="grid grid-cols-2 gap-3 min-[900px]:grid-cols-4">
        <ValueCard icon={<Layers3 size={15} />} label="Активные lanes" value={String(latest?.lanes.length ?? 0)} />
        <ValueCard icon={<Gauge size={15} />} label="RTT" value={`${latest?.lanes[0]?.rttMs ?? 0} ms`} />
        <ValueCard icon={<Boxes size={15} />} label="Backlog" value={formatBytes(latest?.backlogBytes ?? 0)} />
        <ValueCard icon={<Activity size={15} />} label="Blocked writers" value={String(latest?.blockedWriters ?? 0)} />
      </div>

      <ChartCard title="Congestion window и inflight">
        <LineChart
          series={[
            ...laneSeries(points, laneIDs, "congestionWindow", "cwnd"),
            ...laneSeries(points, laneIDs, "inflight", "inflight", true),
          ]}
        />
      </ChartCard>

      <ChartCard title="Окна транспорта">
        <LineChart
          series={[
            ...laneSeries(points, laneIDs, "peerWindow", "peer window"),
            ...laneSeries(points, laneIDs, "effectiveWindow", "effective", true),
          ]}
        />
      </ChartCard>

      <ChartCard title="Оптимальный максимальный payload">
        <LineChart
          series={laneSeries(points, laneIDs, "targetPayload", "payload")}
          formatValue={formatBytes}
        />
      </ChartCard>

      <ChartCard title="RTT: реактивный и базовый">
        <LineChart
          series={[
            ...laneSeries(points, laneIDs, "rttMs", "RTT"),
            ...laneSeries(points, laneIDs, "baseRttMs", "base RTT", true),
          ]}
          formatValue={(value) => `${value.toFixed(0)} ms`}
        />
      </ChartCard>

      <ChartCard title="Скорость и подтверждённый upload">
        <LineChart
          series={[
            scalarSeries(points, "Upload", "#f59e0b", (point) => point.rateUp),
            scalarSeries(points, "Download", "#3b82f6", (point) => point.rateDown),
            scalarSeries(points, "ACK upload", "#22c55e", (point) => point.rateConfirmedTx, true),
          ]}
          formatValue={formatRate}
        />
      </ChartCard>

      <p className="text-[11px] leading-relaxed text-muted">
        Сплошная линия показывает управляющее значение, пунктир — фактическое или
        базовое. История хранит последние 180 секунд текущего подключения.
      </p>
    </div>
  );
}

type LaneNumericKey =
  | "congestionWindow"
  | "inflight"
  | "peerWindow"
  | "effectiveWindow"
  | "targetPayload"
  | "rttMs"
  | "baseRttMs";

function laneSeries(
  points: TransportDebugPoint[],
  laneIDs: number[],
  key: LaneNumericKey,
  label: string,
  dashed = false
): LineSeries[] {
  return laneIDs.map((id, index) => ({
    label: `Lane ${id} · ${label}`,
    color: COLORS[index % COLORS.length],
    dashed,
    values: points.map((point) => {
      const lane = point.lanes.find((item) => item.id === id);
      return lane ? lane[key] : null;
    }),
  }));
}

function scalarSeries(
  points: TransportDebugPoint[],
  label: string,
  color: string,
  select: (point: TransportDebugPoint) => number,
  dashed = false
): LineSeries {
  return { label, color, dashed, values: points.map(select) };
}

function ChartCard({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      {children}
    </Card>
  );
}

function ValueCard({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <Card className="py-4">
      <div className="flex items-center gap-2 text-xs text-muted">
        {icon}
        {label}
      </div>
      <div className="mt-2 font-mono text-lg font-semibold text-fg">{value}</div>
    </Card>
  );
}
