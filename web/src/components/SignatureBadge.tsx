import { Shield, ShieldAlert, ShieldCheck, ShieldX } from "lucide-react";
import type { SignatureReport } from "@/lib/types";
import { cn } from "@/lib/utils";

function listPreview(items: string[] | undefined, total: number): string {
  const shown = items ?? [];
  const more = total - shown.length;
  return shown.slice(0, 5).join(", ") + (shown.length > 5 || more > 0 ? ", …" : "");
}

/** Human-readable one-paragraph summary of a signature report (used as tooltip). */
export function signatureSummary(report: SignatureReport | undefined): string {
  if (!report) return "Signatures not checked yet";
  const keys = report.authorities?.length ? ` · key: ${report.authorities.join(", ")}` : "";
  switch (report.status) {
    case "ok":
      return `All ${report.pboCount} PBOs signed, bikey present${keys}`;
    case "missing_key": {
      const n = report.unmatchedCount ?? report.unmatchedPbos?.length ?? 0;
      const auth = report.missingKeys?.length ? ` for ${report.missingKeys.join(", ")}` : "";
      return `All PBOs signed, but no matching .bikey${auth} (${n} PBO${n === 1 ? "" : "s"} affected: ${listPreview(report.unmatchedPbos, n)})`;
    }
    case "unsigned": {
      const n = report.pboCount - report.signedCount;
      return `${n} of ${report.pboCount} PBO${report.pboCount === 1 ? "" : "s"} without .bisign: ${listPreview(report.unsignedPbos, n)}`;
    }
    case "no_pbos":
      return "No PBOs found in addons/";
    default:
      return "Mod files not found on disk";
  }
}

const styles: Record<string, { Icon: typeof Shield; className: string }> = {
  ok: { Icon: ShieldCheck, className: "text-success" },
  missing_key: { Icon: ShieldAlert, className: "text-warning" },
  unsigned: { Icon: ShieldX, className: "text-destructive" },
};

/**
 * Shield icon for a mod's signature health:
 * green = every PBO signed with a matching bikey, yellow = bikey missing,
 * red = some PBOs unsigned, grey = not checked / nothing to check.
 */
export function SignatureBadge({
  report,
  className,
}: {
  report: SignatureReport | undefined;
  className?: string;
}) {
  const { Icon, className: color } = (report && styles[report.status]) || {
    Icon: Shield,
    className: "text-muted-foreground/50",
  };
  const summary = signatureSummary(report);
  return (
    <span title={summary} aria-label={summary} className="inline-flex shrink-0 align-middle">
      <Icon className={cn("size-4", color, className)} />
    </span>
  );
}
