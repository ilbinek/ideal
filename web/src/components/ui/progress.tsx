import { cn } from "@/lib/utils";

interface ProgressProps {
  value: number;
  className?: string;
  variant?: "default" | "success" | "error";
}

export function Progress({ value, className, variant = "default" }: ProgressProps) {
  const color =
    variant === "success"
      ? "var(--success)"
      : variant === "error"
        ? "var(--destructive)"
        : "var(--primary)";
  return (
    <div className={cn("h-2 w-full overflow-hidden rounded-full bg-muted", className)}>
      <div
        className="h-full rounded-full transition-[width] duration-300 ease-out"
        style={{
          width: `${Math.min(100, Math.max(0, value))}%`,
          backgroundColor: color,
        }}
      />
    </div>
  );
}
