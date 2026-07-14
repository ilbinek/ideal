import { cn } from "@/lib/utils";

type HalfCircleGaugeProps = {
  /** Fill amount from 0 to 1. */
  value: number;
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

/**
 * Animated semicircle gauge. Arc fills left→right as value goes 0→1.
 */
export function HalfCircleGauge({
  value,
  label,
  display,
  detail,
  className,
}: HalfCircleGaugeProps) {
  const clamped = Math.min(1, Math.max(0, Number.isFinite(value) ? value : 0));
  const offset = ARC_LEN * (1 - clamped);

  return (
    <div className={cn("flex w-[7.5rem] flex-col items-center", className)}>
      <div className="relative w-full">
        <svg viewBox="0 0 100 58" className="h-auto w-full" aria-hidden>
          <path
            d={`M ${CX - RADIUS} ${CY} A ${RADIUS} ${RADIUS} 0 0 1 ${CX + RADIUS} ${CY}`}
            fill="none"
            stroke="currentColor"
            strokeWidth={STROKE}
            strokeLinecap="round"
            className="text-muted/40"
          />
          <path
            d={`M ${CX - RADIUS} ${CY} A ${RADIUS} ${RADIUS} 0 0 1 ${CX + RADIUS} ${CY}`}
            fill="none"
            stroke="currentColor"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={ARC_LEN}
            strokeDashoffset={offset}
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
