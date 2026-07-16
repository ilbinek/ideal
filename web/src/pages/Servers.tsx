import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  GripVertical,
  Loader2,
  Play,
  Plus,
  Server,
  Square,
} from "lucide-react";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { patchServerStatus, useServers } from "@/hooks/useServers";
import { permissionsOf, useSession } from "@/hooks/useSession";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import type { GameServer, ServerStatus } from "@/lib/types";

function statusVariant(status: ServerStatus) {
  switch (status) {
    case "running":
      return "success";
    case "starting":
    case "stopping":
      return "secondary";
    case "unknown":
      return "destructive";
    default:
      return "outline";
  }
}

function reorderList(list: GameServer[], fromId: number, toId: number): GameServer[] {
  if (fromId === toId) return list;
  const from = list.findIndex((s) => s.id === fromId);
  const to = list.findIndex((s) => s.id === toId);
  if (from < 0 || to < 0) return list;
  const next = [...list];
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item);
  return next;
}

export function Servers() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { statusFor } = useServers();
  const [actionError, setActionError] = useState("");
  const { data: session } = useSession();
  const perms = permissionsOf(session);
  const canReorder = perms.isFullAdmin;

  const servers = useQuery({ queryKey: ["servers"], queryFn: () => api.getServers() });
  const branches = useQuery({ queryKey: ["branches"], queryFn: () => api.getBranches() });

  const [ordered, setOrdered] = useState<GameServer[]>([]);
  const [dragId, setDragId] = useState<number | null>(null);
  const [overId, setOverId] = useState<number | null>(null);

  useEffect(() => {
    if (servers.data) setOrdered(servers.data);
  }, [servers.data]);

  const start = useMutation({
    mutationFn: (id: number) => api.startServer(id),
    onMutate: (id) => {
      setActionError("");
      patchServerStatus(queryClient, id, "starting");
    },
    onSuccess: (_data, id) => {
      setActionError("");
      patchServerStatus(queryClient, id, "running");
      queryClient.invalidateQueries({ queryKey: ["servers"] });
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
    onError: (err, id) => {
      setActionError(err instanceof Error ? err.message : "Start failed");
      queryClient.invalidateQueries({ queryKey: ["servers"] });
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
  });
  const stop = useMutation({
    mutationFn: (id: number) => api.stopServer(id),
    onMutate: (id) => {
      setActionError("");
      patchServerStatus(queryClient, id, "stopping");
    },
    onSuccess: (_data, id) => {
      setActionError("");
      patchServerStatus(queryClient, id, "stopped");
      queryClient.invalidateQueries({ queryKey: ["servers"] });
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
    onError: (err, id) => {
      setActionError(err instanceof Error ? err.message : "Stop failed");
      queryClient.invalidateQueries({ queryKey: ["servers"] });
      queryClient.invalidateQueries({ queryKey: ["servers", id] });
    },
  });

  const reorder = useMutation({
    mutationFn: (ids: number[]) => api.reorderServers(ids),
    onMutate: async (ids) => {
      setActionError("");
      await queryClient.cancelQueries({ queryKey: ["servers"] });
      const previous = queryClient.getQueryData<GameServer[]>(["servers"]);
      const byId = new Map((previous ?? ordered).map((s) => [s.id, s]));
      const next = ids
        .map((id, i) => {
          const s = byId.get(id);
          return s ? { ...s, sortOrder: i } : null;
        })
        .filter((s): s is GameServer => s != null);
      queryClient.setQueryData(["servers"], next);
      setOrdered(next);
      return { previous };
    },
    onError: (err, _ids, ctx) => {
      if (ctx?.previous) {
        queryClient.setQueryData(["servers"], ctx.previous);
        setOrdered(ctx.previous);
      }
      setActionError(err instanceof Error ? err.message : "Reorder failed");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
  });

  const branchName = (id: number) =>
    branches.data?.find((b) => b.id === id)?.name ?? `Branch #${id}`;

  const commitReorder = (next: GameServer[]) => {
    setOrdered(next);
    reorder.mutate(next.map((s) => s.id));
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Servers"
        description={
          canReorder
            ? "Manage Arma 3 dedicated server instances. Drag the handle to reorder."
            : "Manage Arma 3 dedicated server instances."
        }
        actions={
          <>
            {perms.canManageBranches && (
              <Button variant="outline" asChild>
                <Link to="/branches">Branches</Link>
              </Button>
            )}
            {perms.isFullAdmin && (
              <Button asChild>
                <Link to="/servers/new">
                  <Plus className="size-4" />
                  New server
                </Link>
              </Button>
            )}
          </>
        }
      />

      {actionError && (
        <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {actionError}
        </p>
      )}

      {servers.isLoading ? (
        <div className="flex justify-center py-12">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      ) : !ordered.length ? (
        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Server className="size-4 text-muted-foreground" />
              No servers yet
            </CardTitle>
            <CardDescription>
              Install a branch, then create your first server instance.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex gap-2">
            {perms.canManageBranches && (
              <Button variant="outline" onClick={() => navigate("/branches")}>
                Manage branches
              </Button>
            )}
            {perms.isFullAdmin && (
              <Button onClick={() => navigate("/servers/new")}>Create server</Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col gap-2">
          {ordered.map((srv) => {
            const live = statusFor(srv.id);
            const status = srv.status ?? live?.status ?? "stopped";
            const isRunning = status === "running" || status === "starting";
            return (
              <Card
                key={srv.id}
                className={cn(
                  "flex flex-col gap-0 sm:flex-row sm:items-center",
                  dragId === srv.id && "opacity-60",
                  overId === srv.id && dragId !== srv.id && "border-foreground/40",
                )}
                onDragOver={
                  canReorder
                    ? (e) => {
                        e.preventDefault();
                        e.dataTransfer.dropEffect = "move";
                        if (overId !== srv.id) setOverId(srv.id);
                      }
                    : undefined
                }
                onDragLeave={
                  canReorder
                    ? () => {
                        if (overId === srv.id) setOverId(null);
                      }
                    : undefined
                }
                onDrop={
                  canReorder
                    ? (e) => {
                        e.preventDefault();
                        const from = Number(e.dataTransfer.getData("text/plain"));
                        setDragId(null);
                        setOverId(null);
                        if (!Number.isFinite(from) || from === srv.id) return;
                        commitReorder(reorderList(ordered, from, srv.id));
                      }
                    : undefined
                }
              >
                {canReorder && (
                  <div
                    className="flex cursor-grab items-center px-2 pt-4 text-muted-foreground active:cursor-grabbing sm:pt-0 sm:pl-3"
                    draggable
                    onDragStart={(e) => {
                      e.dataTransfer.effectAllowed = "move";
                      e.dataTransfer.setData("text/plain", String(srv.id));
                      setDragId(srv.id);
                    }}
                    onDragEnd={() => {
                      setDragId(null);
                      setOverId(null);
                    }}
                    title="Drag to reorder"
                  >
                    <GripVertical className="size-4" />
                  </div>
                )}
                <CardHeader className="flex-1 pb-2 sm:pb-4">
                  <div className="flex flex-wrap items-center gap-2">
                    <CardTitle className="text-base">
                      <Link
                        to={`/servers/${srv.id}`}
                        className="transition-colors hover:underline"
                      >
                        {srv.name}
                      </Link>
                    </CardTitle>
                    <Badge variant={statusVariant(status)}>{status}</Badge>
                  </div>
                  <CardDescription className="font-mono text-xs">
                    {branchName(srv.branchId)} · :{srv.gamePort}
                    {live?.uptime ? ` · ${live.uptime}s uptime` : ""}
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex gap-2 sm:pt-4">
                  <Button
                    size="sm"
                    variant={isRunning ? "outline" : "default"}
                    disabled={start.isPending || stop.isPending}
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
