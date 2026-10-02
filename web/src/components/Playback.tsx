import { useEffect, useRef, useState } from "react";
import { Pause, Play, RotateCcw } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { PointRow } from "../lib/api";
import { dateTime } from "../lib/format";
import { Select, Slider } from "./controls";
import { Button } from "./ui";

const speeds = ["1", "2", "5", "10"] as const;
const BASE_SECONDS = 60; // at 1x, the whole range plays in one minute

/** Position at time `ts`, interpolated between the surrounding points. */
function positionAt(points: PointRow[], ts: number): { lon: number; lat: number } {
  let lo = 0, hi = points.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (points[mid][0] < ts) lo = mid + 1;
    else hi = mid;
  }
  const b = points[lo], a = points[Math.max(0, lo - 1)];
  const f = b[0] === a[0] ? 1 : Math.min(1, Math.max(0, (ts - a[0]) / (b[0] - a[0])));
  return { lat: a[1] + (b[1] - a[1]) * f, lon: a[2] + (b[2] - a[2]) * f };
}

/** Replays movement over a range: animated head marker, scrubber and speed control. */
export function Playback({ points, clock, onPosition }: { points: PointRow[]; clock: "24h" | "12h"; onPosition: (p: { lon: number; lat: number } | null) => void }) {
  const { t } = useTranslation();
  const start = points[0]?.[0] ?? 0, end = points.at(-1)?.[0] ?? 0;
  const [time, setTime] = useState(start);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<(typeof speeds)[number]>("1");
  const raf = useRef(0);

  useEffect(() => {
    setTime(start);
    setPlaying(false);
    onPosition(null);
  }, [start, end]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!playing) return;
    let last = performance.now();
    const rate = ((end - start) / (BASE_SECONDS * 1000)) * Number(speed);
    const tick = (now: number) => {
      const dt = now - last;
      last = now;
      setTime((cur) => {
        const next = Math.min(end, cur + dt * rate);
        if (next >= end) setPlaying(false);
        return next;
      });
      raf.current = requestAnimationFrame(tick);
    };
    raf.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf.current);
  }, [playing, speed, start, end]);

  useEffect(() => {
    if (points.length > 1 && (playing || time !== start)) onPosition(positionAt(points, time));
  }, [time]); // eslint-disable-line react-hooks/exhaustive-deps

  if (points.length < 2) return null;
  const done = time >= end;
  return (
    <div className="flex items-center gap-3 rounded-2xl border border-border bg-surface/95 p-2.5 shadow-pop backdrop-blur-sm">
      <Button
        variant="primary"
        size="icon"
        className="rounded-xl"
        onClick={() => {
          if (done) setTime(start);
          setPlaying(!playing || done);
        }}
        aria-label={playing ? t("playback.pause") : t("playback.play")}
      >
        {playing ? <Pause className="size-5" /> : done ? <RotateCcw className="size-5" /> : <Play className="size-5" />}
      </Button>
      <div className="min-w-0 flex-1">
        <p className="mb-1 truncate text-xs font-medium text-muted tabular-nums">{dateTime(time, clock)}</p>
        <Slider label={t("playback.position")} min={start} max={end} step={Math.max(1000, Math.round((end - start) / 1000))} value={time}
          onChange={(v) => { setPlaying(false); setTime(v); }} format={(v) => dateTime(v, clock)} />
      </div>
      <div className="w-20 shrink-0">
        <Select label={t("playback.speed")} value={speed} onChange={setSpeed} options={speeds.map((s) => ({ value: s, label: `${s}×` }))} />
      </div>
    </div>
  );
}
