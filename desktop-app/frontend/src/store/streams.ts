import { create } from "zustand";
import type { TcpStream } from "@/types";
import type { StreamMetric } from "@/lib/backend";

/**
 * Стор TCP-стримов для экрана «Статистика».
 *
 * Заполняется из метрик туннеля (событие tunnel:metrics). Бэкенд присылает только
 * активные стримы; те, что исчезли из снапшота, помечаются закрытыми и через
 * короткую задержку удаляются (для плавного исчезновения в UI).
 */
interface StreamsState {
  streams: TcpStream[];
  /** Кол-во уникальных стримов за сессию. */
  totalCreated: number;

  /** Применяет снапшот активных стримов из метрик. */
  applyMetrics: (list: StreamMetric[]) => void;
  /** Сбрасывает все стримы (при отключении туннеля). */
  reset: () => void;
}

/** Через сколько мс после закрытия удалять неактивный стрим. */
const PRUNE_DELAY = 4000;

/** Сортировка: активные — вперёд, затем по убыванию трафика. */
function sortStreams(list: TcpStream[]): TcpStream[] {
  return [...list].sort((a, b) => {
    if (a.active !== b.active) return a.active ? -1 : 1;
    const ta = a.totalUp + a.totalDown;
    const tb = b.totalUp + b.totalDown;
    return tb - ta;
  });
}

function mapStream(d: StreamMetric, startedAt: number): TcpStream {
  return {
    id: String(d.id),
    target: d.target,
    host: d.host || undefined,
    active: true,
    startedAt: startedAt || d.startedAt || Date.now(),
    closedAt: null,
    totalUp: d.totalUp,
    totalDown: d.totalDown,
    rateUp: d.rateUp,
    rateDown: d.rateDown,
  };
}

export const useStreamsStore = create<StreamsState>((set) => ({
  streams: [],
  totalCreated: 0,

  applyMetrics: (list) =>
    set((s) => {
      const now = Date.now();
      const incoming = new Map(list.map((d) => [String(d.id), d]));
      const seen = new Set<string>();
      let totalCreated = s.totalCreated;
      const next: TcpStream[] = [];

      // Обновляем/закрываем существующие.
      for (const st of s.streams) {
        const d = incoming.get(st.id);
        if (d && st.active) {
          seen.add(st.id);
          next.push(mapStream(d, st.startedAt));
        } else if (st.active) {
          // Исчез из снапшота → только что закрылся.
          next.push({ ...st, active: false, closedAt: now, rateUp: 0, rateDown: 0 });
        } else if (st.closedAt && now - st.closedAt < PRUNE_DELAY) {
          next.push(st); // ещё показываем закрытый
        }
      }

      // Новые стримы.
      for (const d of list) {
        if (!seen.has(String(d.id))) {
          totalCreated += 1;
          next.push(mapStream(d, d.startedAt));
        }
      }

      return { streams: sortStreams(next), totalCreated };
    }),

  reset: () => set({ streams: [], totalCreated: 0 }),
}));
