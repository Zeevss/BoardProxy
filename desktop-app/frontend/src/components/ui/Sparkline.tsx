import { useId } from "react";
import { cn } from "@/lib/utils";

interface SparklineProps {
  data: number[];
  /** Цвет линии (rgb-переменная или произвольный). */
  color?: string;
  className?: string;
  width?: number;
  height?: number;
}

/**
 * Мини-график (sparkline) на чистом SVG — без зависимостей.
 * Рисует сглаженную линию + полупрозрачную заливку под ней.
 */
export function Sparkline({
  data,
  color = "rgb(var(--c-accent))",
  className,
  width = 240,
  height = 56,
}: SparklineProps) {
  const gradId = useId();
  const n = data.length;

  // Падаем к пустому графику при недостатке данных.
  if (n < 2) {
    return (
      <div
        className={cn("flex items-center justify-center text-[11px] text-muted/60", className)}
        style={{ width, height }}
      >
        нет данных
      </div>
    );
  }

  const max = Math.max(...data, 1);
  const min = Math.min(...data, 0);
  const span = max - min || 1;
  const stepX = width / (n - 1);

  const points = data.map((v, i) => {
    const x = i * stepX;
    const y = height - ((v - min) / span) * (height - 6) - 3;
    return [x, y] as const;
  });

  // Сглаженный путь (Catmull-Rom → bezier).
  const line = smoothPath(points);
  const area = `${line} L ${width},${height} L 0,${height} Z`;

  return (
    <svg
      className={className}
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
    >
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.35" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#${gradId})`} />
      <path
        d={line}
        fill="none"
        stroke={color}
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

/** Строит сглаженный SVG-путь через точки. */
function smoothPath(points: ReadonlyArray<readonly [number, number]>): string {
  if (points.length === 0) return "";
  if (points.length === 1) return `M ${points[0][0]},${points[0][1]}`;

  let d = `M ${points[0][0]},${points[0][1]}`;
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i === 0 ? 0 : i - 1];
    const p1 = points[i];
    const p2 = points[i + 1];
    const p3 = points[i + 2 < points.length ? i + 2 : i + 1];

    const cp1x = p1[0] + (p2[0] - p0[0]) / 6;
    const cp1y = p1[1] + (p2[1] - p0[1]) / 6;
    const cp2x = p2[0] - (p3[0] - p1[0]) / 6;
    const cp2y = p2[1] - (p3[1] - p1[1]) / 6;

    d += ` C ${cp1x},${cp1y} ${cp2x},${cp2y} ${p2[0]},${p2[1]}`;
  }
  return d;
}
