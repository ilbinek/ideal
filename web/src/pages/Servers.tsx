import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Loader2,
  Play,
  Plus,
  Server,
  Square,
} from "lucide-react";
import { api } from "@/lib/api";
import { patchServerStatus, useServers } from "@/hooks/useServers";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import type { ServerStatus } from "@/lib/types";

function statusVariant(status: ServerStatus) {
  switch (status) {
    case "running":
      return "default";
    case "starting":
    case "stopping":
      return "secondary";
    case "unknown":
      return "destructive";
    default:
      return "outline";
  }
}

export function Servers() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { statusFor } = useServers();

  const servers = useQuery({ queryKey: ["servers"], queryFn: () => api.getServers() });
  const branches = useQuery({ queryKey: ["branches"], queryFn: () => api.getBranches() });

  const start = useMutation({
    mutationFn: (id: number) => api.startServer(id),
    onMutate: (id) => patchServerStatus(queryClient, id, "starting"),
    onSuccess: (_data, id) => {
      patchServerStatus(queryClient, id, "running");
      queryClient.invalidateQueries({ queryKey: ["servers"] });
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
    onError: (_err, id) => {
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
  });
  const stop = useMutation({
    mutationFn: (id: number) => api.stopServer(id),
    onMutate: (id) => patchServerStatus(queryClient, id, "stopping"),
    onSuccess: (_data, id) => {
      patchServerStatus(queryClient, id, "stopped");
      queryClient.invalidateQueries({ queryKey: ["servers"] });
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
    onError: (_err, id) => {
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
  });

  const branchName = (id: number) =>
    branches.data?.find((b) => b.id === id)?.name ?? `Branch #${id}`;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Servers</h1>
          <p className="text-sm text-muted-foreground">
            Manage Arma 3 dedicated server instances.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" asChild>
            <Link to="/branches">Branches</Link>
          </Button>
          <Button asChild>
            <Link to="/servers/new">
              <Plus className="size-4" />
              New server
            </Link>
          </Button>
        </div>
      </div>

      {servers.isLoading ? (
        <div className="flex justify-center py-12">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      ) : !servers.data?.length ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Server className="size-4" />
              No servers yet
            </CardTitle>
            <CardDescription>
              Install a branch, then create your first server instance.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex gap-2">
            <Button variant="outline" onClick={() => navigate("/branches")}>
              Manage branches
            </Button>
            <Button onClick={() => navigate("/servers/new")}>Create server</Button>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {servers.data.map((srv) => {
            const live = statusFor(srv.id);
            const status = srv.status ?? live?.status ?? "stopped";
            const isRunning = status === "running" || status === "starting";
            return (
              <Card key={srv.id} className="flex flex-col">
                <CardHeader className="pb-2">
                  <div className="flex items-start justify-between gap-2">
                    <CardTitle className="text-base">
                      <Link
                        to={`/servers/${srv.id}`}
                        className="hover:underline"
                      >
                        {srv.name}
                      </Link>
                    </CardTitle>
                    <Badge variant={statusVariant(status)}>{status}</Badge>
                  </div>
                  <CardDescription>
                    {branchName(srv.branchId)} · :{srv.gamePort}
                    {live?.uptime ? ` · ${live.uptime}s uptime` : ""}
                  </CardDescription>
                </CardHeader>
                <CardContent className="mt-auto flex gap-2">
                  <Button
                    size="sm"
                    variant={isRunning ? "outline" : "default"}
                    disabled={start.isPending || stop.isPending || status === "stopping"}
                    onClick={() =>
                      isRunning ? stop.mutate(srv.id) : start.mutate(srv.id)
                    }
                  >
                    {isRunning ? (
                      <>
                        <Square className="size-3.5" /> Stop
                      </>
                    ) : (
                      <>
                        <Play className="size-3.5" /> Start
                      </>
                    )}
                  </Button>
                  <Button size="sm" variant="outline" asChild>
                    <Link to={`/servers/${srv.id}`}>Manage</Link>
                  </Button>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}
    </div>
  );
}
