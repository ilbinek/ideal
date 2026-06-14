import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowRight, Boxes, CheckCircle2, Package, Server } from "lucide-react";
import { api } from "@/lib/api";
import { formatDate } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function Dashboard() {
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => api.getSettings() });
  const modsets = useQuery({ queryKey: ["modsets"], queryFn: () => api.getModsets() });
  const servers = useQuery({ queryKey: ["servers"], queryFn: () => api.getServers() });

  const steamConfigured = !!settings.data?.username && !!settings.data?.apiKey;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold">Dashboard</h1>
        <p className="text-sm text-muted-foreground">
          Manage Arma 3 dedicated servers, Workshop mods, and modsets.
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              {steamConfigured ? (
                <CheckCircle2 className="size-4 text-[var(--success)]" />
              ) : (
                <AlertTriangle className="size-4 text-[var(--warning)]" />
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

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Server className="size-4 text-primary" />
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

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Package className="size-4 text-primary" />
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
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2 text-base">
              <Boxes className="size-4 text-primary" />
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
                <li key={m.id} className="flex items-center justify-between py-2">
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
    </div>
  );
}
