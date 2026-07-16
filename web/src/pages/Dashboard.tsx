import type { ComponentType } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  Boxes,
  CheckCircle2,
  Package,
  Server,
  ShieldAlert,
} from "lucide-react";
import { api } from "@/lib/api";
import { cn, formatDate } from "@/lib/utils";
import { permissionsOf, useSession } from "@/hooks/useSession";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function Dashboard() {
  const { data: session } = useSession();
  const perms = permissionsOf(session);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.getSettings(),
    enabled: perms.isFullAdmin,
  });
  const modsets = useQuery({
    queryKey: ["modsets"],
    queryFn: () => api.getModsets(),
    enabled: perms.canManageMods || perms.canAccessServers,
  });
  const servers = useQuery({
    queryKey: ["servers"],
    queryFn: () => api.getServers(),
    enabled: perms.canAccessServers,
  });

  if (!perms.hasAnyAccess) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Welcome"
          description={
            <>
              You are signed in
              {session?.user?.displayName ? ` as ${session.user.displayName}` : ""}.
            </>
          }
        />
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldAlert className="size-4 text-warning" />
              Waiting for access
            </CardTitle>
            <CardDescription>
              An administrator must grant you permissions before you can manage
              servers, mods, or branches.
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  const steamConfigured = !!settings.data?.username && !!settings.data?.apiKey;
  const serverCount = servers.data?.length ?? 0;
  const runningCount =
    servers.data?.filter((s) => s.status === "running" || s.status === "starting")
      .length ?? 0;
  const modsetCount = modsets.data?.length ?? 0;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Dashboard"
        description="Manage Arma 3 dedicated servers, Workshop mods, and modsets."
      />

      <div
        className={cn(
          "grid gap-3",
          perms.isFullAdmin && perms.canAccessServers && (perms.canManageMods || perms.canAccessServers)
            ? "sm:grid-cols-3"
            : "sm:grid-cols-2",
        )}
      >
        {perms.canAccessServers && (
          <StatTile
            label="Servers"
            value={String(serverCount)}
            hint={
              serverCount
                ? `${runningCount} running`
                : "None configured"
            }
            icon={Server}
            accent={runningCount > 0}
          />
        )}
        {(perms.canManageMods || perms.canAccessServers) && (
          <StatTile
            label="Modsets"
            value={String(modsetCount)}
            hint={modsetCount ? "Presets imported" : "No modsets yet"}
            icon={Boxes}
          />
        )}
        {perms.isFullAdmin && (
          <StatTile
            label="Steam"
            value={steamConfigured ? "Ready" : "Setup"}
            hint={
              steamConfigured
                ? "Credentials configured"
                : "Credentials missing"
            }
            icon={steamConfigured ? CheckCircle2 : AlertTriangle}
            accent={steamConfigured}
            warn={!steamConfigured}
          />
        )}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {perms.isFullAdmin && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                {steamConfigured ? (
                  <CheckCircle2 className="size-4 text-success" />
                ) : (
                  <AlertTriangle className="size-4 text-warning" />
                )}
                Steam configuration
              </CardTitle>
              <CardDescription>
                {steamConfigured
                  ? "Steam credentials and API key are set."
                  : "Steam credentials or API key are missing."}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button asChild variant={steamConfigured ? "outline" : "default"} size="sm">
                <Link to="/settings">
                  Configure <ArrowRight className="size-4" />
                </Link>
              </Button>
            </CardContent>
          </Card>
        )}

        {perms.canAccessServers && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Server className="size-4 text-muted-foreground" />
                Servers
              </CardTitle>
              <CardDescription>
                {servers.data?.length
                  ? `${servers.data.length} server instance(s) configured.`
                  : "No servers yet — install a branch first."}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button asChild variant="outline" size="sm">
                <Link to="/servers">
                  Manage servers <ArrowRight className="size-4" />
                </Link>
              </Button>
            </CardContent>
          </Card>
        )}

        {perms.canManageMods && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Package className="size-4 text-muted-foreground" />
                Mods
              </CardTitle>
              <CardDescription>Browse and download all mods.</CardDescription>
            </CardHeader>
            <CardContent>
              <Button asChild variant="outline" size="sm">
                <Link to="/mods">
                  View mods <ArrowRight className="size-4" />
                </Link>
              </Button>
            </CardContent>
          </Card>
        )}
      </div>

      {perms.canManageMods && (
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div>
              <CardTitle className="flex items-center gap-2 text-base">
                <Boxes className="size-4 text-muted-foreground" />
                Recent modsets
              </CardTitle>
              <CardDescription>Imported Arma 3 launcher presets.</CardDescription>
            </div>
            <Button asChild variant="ghost" size="sm">
              <Link to="/modsets">All modsets</Link>
            </Button>
          </CardHeader>
          <CardContent>
            {modsets.data && modsets.data.length > 0 ? (
              <ul className="flex flex-col divide-y divide-border">
                {modsets.data.slice(0, 5).map((m) => (
                  <li key={m.id} className="flex items-center justify-between py-2.5">
                    <div>
                      <p className="text-sm font-medium">{m.name}</p>
                      <p className="text-xs text-muted-foreground">
                        Created {formatDate(m.createdAt)}
                      </p>
                    </div>
                    <Button asChild variant="link" size="sm">
                      <Link to={`/mods?modset=${m.id}`}>Open</Link>
                    </Button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">
                No modsets yet. Upload one from the Modsets page.
              </p>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function StatTile({
  label,
  value,
  hint,
  icon: Icon,
  accent,
  warn,
}: {
  label: string;
  value: string;
  hint: string;
  icon: ComponentType<{ className?: string }>;
  accent?: boolean;
  warn?: boolean;
}) {
  return (
    <div
      className={cn(
        "rounded-lg border border-border bg-card px-4 py-3",
        warn && "border-warning/30",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </span>
        <Icon
          className={cn(
            "size-3.5",
            warn ? "text-warning" : accent ? "text-success" : "text-muted-foreground",
          )}
        />
      </div>
      <p className="mt-1.5 text-2xl font-semibold tracking-tight">{value}</p>
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}
