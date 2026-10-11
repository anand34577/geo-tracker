import { useId, useMemo, useRef, useState } from "react";
import { cn } from "./ui";

/** A compact area chart over time with a hover/touch readout. SVG only, no chart library. */
export function AreaChart({ data, label, formatY, formatX, height = 88, yMin, yMax, className }: {
  /** [x, y] pairs, x ascending (usually ms timestamps). */
  data: [number, number][];
  label: string;
  formatY: (y: number) => string;
  formatX: (x: number) => string;
  height?: number;
  /** Fixed axis bounds (e.g. 0–100 for battery); otherwise fitted to the data. */
  yMin?: number;
  yMax?: number;
  className?: string;
}) {
  const gradient = useId();
  const box = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<number | null>(null);

  const g = useMemo(() => {
    const xs = data.map((d) => d[0]), ys = data.map((d) => d[1]);
    const x0 = xs[0] ?? 0, x1 = xs.at(-1) ?? 1;
    let y0 = yMin ?? Math.min(...ys), y1 = yMax ?? Math.max(...ys);
    if (y1 - y0 < 1e-9) { y0 -= 1; y1 += 1; } // flat line: give it room
    const pad = yMax === undefined ? (y1 - y0) * 0.08 : 0;
    y1 += pad;
    const px = (x: number) => (x1 === x0 ? 500 : ((x - x0) / (x1 - x0)) * 1000);
    const py = (y: number) => 100 - ((y - y0) / (y1 - y0)) * 100;
    const line = data.map(([x, y], i) => `${i ? "L" : "M"}${px(x).toFixed(1)},${py(y).toFixed(1)}`).join("");
    return { px, py, line, area: data.length ? `${line}L${px(x1)},100L${px(x0)},100Z` : "", min: Math.min(...ys), max: Math.max(...ys) };
  }, [data, yMin, yMax]);

  if (data.length < 2) return null;
  const at = (clientX: number) => {
    const r = box.current!.getBoundingClientRect();
    const x = data[0][0] + ((clientX - r.left) / r.width) * (data.at(-1)![0] - data[0][0]);
    let lo = 0, hi = data.length - 1;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (data[mid][0] < x) lo = mid + 1;
      else hi = mid;
    }
    setHover(lo > 0 && x - data[lo - 1][0] < data[lo][0] - x ? lo - 1 : lo);
  };
  const h = hover != null ? data[hover] : null;

  return (
    <figure className={cn("relative", className)}>
      <div
        ref={box}
        className="relative touch-none"
        style={{ height }}
        onPointerMove={(e) => at(e.clientX)}
        onPointerDown={(e) => at(e.clientX)}
        onPointerLeave={() => setHover(null)}
        role="img"
        aria-label={`${label}: ${formatY(g.min)} – ${formatY(g.max)}`}
      >
        <svg viewBox="0 0 1000 100" preserveAspectRatio="none" className="absolute inset-0 size-full overflow-visible">
          <defs>
            <linearGradient id={gradient} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0" stopColor="var(--primary)" stopOpacity="0.32" />
              <stop offset="1" stopColor="var(--primary)" stopOpacity="0.02" />
            </linearGradient>
          </defs>
          <path d={g.area} fill={`url(#${gradient})`} />
          <path d={g.line} fill="none" stroke="var(--primary)" strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
          {h && <line x1={g.px(h[0])} x2={g.px(h[0])} y1="0" y2="100" stroke="var(--fg-subtle)" strokeWidth="1" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />}
        </svg>
        {h && (
          <>
            <span className="pointer-events-none absolute size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-surface bg-primary shadow" style={{ left: `${g.px(h[0]) / 10}%`, top: `${g.py(h[1])}%` }} />
            <span
              className="pointer-events-none absolute -top-1 z-10 -translate-y-full rounded-lg border border-border bg-surface px-2 py-1 text-xs whitespace-nowrap shadow-pop tabular-nums"
              style={{ left: `clamp(0px, calc(${g.px(h[0]) / 10}% - 3rem), calc(100% - 6rem))` }}
            >
              <span className="font-semibold">{formatY(h[1])}</span> <span className="text-subtle">· {formatX(h[0])}</span>
            </span>
          </>
        )}
      </div>
      <figcaption className="mt-1 flex justify-between text-[11px] text-subtle tabular-nums">
        <span>{formatX(data[0][0])}</span>
        <span>{label} · {formatY(g.min)} – {formatY(g.max)}</span>
        <span>{formatX(data.at(-1)![0])}</span>
      </figcaption>
    </figure>
  );
}
