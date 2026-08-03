// formatBytes — человекочитаемый размер (КиБ/МиБ/ГиБ, двоичные степени).
export function formatBytes(n: number): string {
  if (!n) return "0 Б";
  const units = ["Б", "КиБ", "МиБ", "ГиБ", "ТиБ"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  const v = n / Math.pow(1024, i);
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatRate(n: number): string {
  return `${formatBytes(n)}/с`;
}

export function formatDuration(ms: number): string {
  if (!ms) return "0 с";
  if (ms < 1000) return `${Math.round(ms)} мс`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)} с`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  return `${minutes} мин ${rest} с`;
}

// formatDate — дата/время в локали ru, короткий вид.
export function formatDate(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// formatTime — только время (для потока логов).
export function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString("ru-RU", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
