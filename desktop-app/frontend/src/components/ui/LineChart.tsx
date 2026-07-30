import { useId } from "react";

export interface LineSeries {
  label: string;
  color: string;
  values: Array<number | null>;
  dashed?: boolean;
}

export function LineChart({
  series,
  formatValue = (value) => value.toFixed(0),
  height = 210,
}: {
  series: LineSeries[];
  formatValue?: (value: number) => string;
  height?: number;
}) {
  const clipID = useId();
  const width = 760;
  const left = 48;
  const top = 12;
  const bottom = 24;
  const plotWidth = width - left - 12;
  const plotHeight = height - top - bottom;
  const count = Math.max(0, ...series.map((item) => item.values.length));
  const max = Math.max(
    1,
    ...series.flatMap((item) =>
      item.values.filter((value): value is number => value !== null)
    )
  );

  if (count < 2 || series.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-xs text-muted/60"
        style={{ height }}
      >
        Ожидание метрик…
      </div>
    );
  }

  const x = (index: number) => left + (index / Math.max(1, count - 1)) * plotWidth;
  const y = (value: number) => top + plotHeight - (value / max) * plotHeight;

  return (
    <div>
      <svg className="w-full" style={{ height }} viewBox={`0 0 ${width} ${height}`}>
        <defs>
          <clipPath id={clipID}>
            <rect x={left} y={top} width={plotWidth} height={plotHeight} />
          </clipPath>
        </defs>
        {[0, 0.25, 0.5, 0.75, 1].map((ratio) => (
          <g key={ratio}>
            <line
              x1={left}
              x2={width - 12}
              y1={top + plotHeight * ratio}
              y2={top + plotHeight * ratio}
              stroke="rgb(var(--c-border))"
              strokeWidth="1"
            />
            <text
              x={left - 7}
              y={top + plotHeight * ratio + 4}
              textAnchor="end"
              className="fill-muted text-[10px]"
            >
              {formatValue(max * (1 - ratio))}
            </text>
          </g>
        ))}
        <g clipPath={`url(#${clipID})`}>
          {series.flatMap((item) =>
            segments(item.values).map((segment, index) => (
              <polyline
                key={`${item.label}-${index}`}
                points={segment.map(([i, value]) => `${x(i)},${y(value)}`).join(" ")}
                fill="none"
                stroke={item.color}
                strokeWidth="2"
                strokeDasharray={item.dashed ? "6 5" : undefined}
                strokeLinecap="round"
                strokeLinejoin="round"
                vectorEffect="non-scaling-stroke"
              />
            ))
          )}
        </g>
      </svg>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
        {series.map((item) => {
          const latest = [...item.values].reverse().find((value) => value !== null);
          return (
            <div key={item.label} className="flex items-center gap-1.5 text-[11px] text-muted">
              <span
                className="h-0.5 w-4"
                style={{
                  background: item.dashed
                    ? `repeating-linear-gradient(90deg, ${item.color} 0 5px, transparent 5px 8px)`
                    : item.color,
                }}
              />
              <span>{item.label}</span>
              <span className="font-mono text-fg">
                {latest == null ? "—" : formatValue(latest)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function segments(values: Array<number | null>): Array<Array<[number, number]>> {
  const result: Array<Array<[number, number]>> = [];
  let current: Array<[number, number]> = [];
  values.forEach((value, index) => {
    if (value === null) {
      if (current.length > 1) result.push(current);
      current = [];
      return;
    }
    current.push([index, value]);
  });
  if (current.length > 1) result.push(current);
  return result;
}
