import { cn } from "@/lib/utils";

type HalfCircleGaugeProps = {
  /** Primary fill amount from 0 to 1 (process / series of interest). */
  value: number;
  /**
   * Optional host/system fill 0–1 drawn underneath in grey so you can see
   * overall headroom on the same scale.
   */
  hostValue?: number;
  label: string;
  /** Primary value shown in the center (e.g. "42%"). */
  display: string;
  /** Optional secondary line under display. */
  detail?: string;
  className?: string;
};

const RADIUS = 42;
const STROKE = 8;
const CX = 50;
const CY = 48;
/** Semicircle path length = π * r */
const ARC_LEN = Math.PI * RADIUS;

const ARC_PATH = `M ${CX - RADIUS} ${CY} A ${RADIUS} ${RADIUS} 0 0 1 ${CX + RADIUS} ${CY}`;

function clamp01(n: number): number {
  if (!Number.isFinite(n)) return 0;
  return Math.min(1, Math.max(0, n));
}

/**
 * Animated semicircle gauge. Arc fills left→right as value goes 0→1.
 * Optional hostValue draws a grey underlay for total system usage.
 */
export function HalfCircleGauge({
  value,
  hostValue,
  label,
  display,
  detail,
  className,
}: HalfCircleGaugeProps) {
  const primary = clamp01(value);
  const host = hostValue != null ? clamp01(hostValue) : null;
  const primaryOffset = ARC_LEN * (1 - primary);
  const hostOffset = host != null ? ARC_LEN * (1 - host) : 0;

  return (
    <div className={cn("flex w-[7.5rem] flex-col items-center", className)}>
      <div className="relative w-full">
        <svg viewBox="0 0 100 58" className="h-auto w-full" aria-hidden>
          <path
            d={ARC_PATH}
            fill="none"
            stroke="currentColor"
            strokeWidth={STROKE}
            strokeLinecap="round"
            className="text-muted/30"
          />
          {host != null && (
            <path
              d={ARC_PATH}
              fill="none"
              stroke="currentColor"
              strokeWidth={STROKE}
              strokeLinecap="round"
              strokeDasharray={ARC_LEN}
              strokeDashoffset={hostOffset}
              className="text-muted-foreground/55 transition-[stroke-dashoffset] duration-700 ease-out"
            />
          )}
          <path
            d={ARC_PATH}
            fill="none"
            stroke="currentColor"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={ARC_LEN}
            strokeDashoffset={primaryOffset}
            className="text-primary transition-[stroke-dashoffset] duration-700 ease-out"
          />
        </svg>
        <div className="pointer-events-none absolute inset-x-0 bottom-0 flex flex-col items-center pb-0.5">
          <span className="text-lg font-semibold tabular-nums leading-none tracking-tight">
            {display}
          </span>
          {detail ? (
            <span className="mt-0.5 max-w-full truncate text-[10px] text-muted-foreground tabular-nums">
              {detail}
            </span>
          ) : null}
        </div>
      </div>
      <span className="mt-1 text-xs font-medium text-muted-foreground">{label}</span>
    </div>
  );
}
