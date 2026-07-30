import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { Loader2 } from "lucide-react";

// ClientConnections показывает живой снимок соединений клиента (страницы и
// открытые в них стримы). Обновляется чаще списка — это оперативная картина.
export function ClientConnections({ clientId }: { clientId: number }) {
  const { data, isLoading } = useQuery({
    queryKey: ["client-connections", clientId],
    queryFn: () => api.clientConnections(clientId),
    refetchInterval: 2000,
  });

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-3 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> Загрузка соединений…
      </div>
    );
  }

  if (!data?.length) {
    return <div className="py-3 text-sm text-muted-foreground">Клиент сейчас не в сети</div>;
  }

  return (
    <div className="space-y-3 py-2">
      {data.map((conn, i) => (
        <div key={conn.bundle_id ?? i} className="rounded-md border bg-background p-3">
          <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            {conn.bundle_id && (
              <span className="font-mono text-xs text-muted-foreground">
                bundle {conn.bundle_id.slice(0, 8)} · {conn.lanes?.length ?? 1} lane · epoch {conn.epoch}
              </span>
            )}
            <span className="text-muted-foreground">RTT {conn.rtt_ms} мс</span>
            <span className="text-muted-foreground">
              ↑ {formatBytes(conn.received)} · ↓ {formatBytes(conn.written)}
            </span>
          </div>
          <div className="mb-2 flex flex-wrap gap-2">
            {(conn.lanes?.length ? conn.lanes : [{ id: conn.lane_id ?? 1, page: conn.page, rtt_ms: conn.rtt_ms }]).map(
              (lane) => (
                <span key={lane.id} className="rounded bg-muted px-2 py-1 font-mono text-xs text-muted-foreground">
                  lane {lane.id} · стр. {lane.page} · {lane.rtt_ms} мс
                </span>
              ),
            )}
          </div>
          {conn.streams.length > 0 && (
            <div className="space-y-1">
              {conn.streams.map((s) => (
                <div
                  key={s.id}
                  className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/50 px-2 py-1 text-xs"
                >
                  <span className="font-mono">{s.target}</span>
                  <span className="text-muted-foreground">
                    ↑ {formatBytes(s.received)} · ↓ {formatBytes(s.written)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
