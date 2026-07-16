import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Download,
  ExternalLink,
  Loader2,
  RotateCw,
  Trash2,
} from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { formatDate } from "@/lib/utils";
import type { ServerStatus } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useToast } from "@/components/ui/toast";
import { useDownloads } from "@/hooks/useDownloads";

function statusVariant(
  status: ServerStatus,
): "default" | "secondary" | "success" | "warning" | "destructive" {
  switch (status) {
    case "running":
      return "success";
    case "starting":
    case "stopping":
      return "warning";
    case "stopped":
      return "secondary";
    default:
      return "secondary";
  }
}

export function ModDetail() {
  const { id } = useParams<{ id: string }>();
  const modId = parseInt(id ?? "0", 10);
  const navigate = useNavigate();
  const toast = useToast();
  const queryClient = useQueryClient();
  const { itemFor } = useDownloads();
  const [confirmDelete, setConfirmDelete] = useState(false);

  const mod = useQuery({
    queryKey: ["mods", modId],
    queryFn: () => api.getMod(modId),
    enabled: modId > 0,
  });

  const item = itemFor(modId);

  const download = useMutation({
    mutationFn: () => api.downloadMod(modId),
    onError: (e) =>
      toast.error("Download failed", e instanceof ApiError ? e.message : undefined),
  });

  const remove = useMutation({
    mutationFn: () => api.deleteMod(modId),
    onSuccess: () => {
      toast.success("Mod deleted");
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      navigate("/mods");
    },
    onError: (e) =>
      toast.error("Delete failed", e instanceof ApiError ? e.message : undefined),
  });

  if (!modId) {
    return <p className="text-sm text-destructive">Invalid mod id.</p>;
  }

  if (mod.isLoading) {
    return (
      <div className="flex items-center justify-center p-16 text-muted-foreground">
        <Loader2 className="size-5 animate-spin" />
      </div>
    );
  }

  if (mod.isError || !mod.data) {
    return (
      <div className="flex flex-col gap-4">
        <Button asChild variant="ghost" size="sm" className="w-fit">
          <Link to="/mods">
            <ArrowLeft className="size-4" /> Back to mods
          </Link>
        </Button>
        <p className="text-sm text-destructive">
          {mod.error instanceof ApiError ? mod.error.message : "Mod not found."}
        </p>
      </div>
    );
  }

  const data = mod.data;
  const busy =
    download.isPending || item?.status === "queued" || item?.status === "downloading";

  let dlLabel = data.downloaded ? "Redownload" : "Download";
  let DlIcon = data.downloaded ? RotateCw : Download;
  if (data.updateAvailable) {
    dlLabel = "Update";
    DlIcon = RotateCw;
  }
  if (item?.status === "queued") dlLabel = "Queued";
  if (item?.status === "downloading") dlLabel = "Downloading";

  const serverNames = data.servers.map((s) => s.name);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-start gap-3">
          <Button asChild variant="ghost" size="icon">
            <Link to="/mods">
              <ArrowLeft className="size-4" />
            </Link>
          </Button>
          <div className="flex flex-col gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">{data.name}</h1>
            <div className="flex flex-wrap items-center gap-2">
              <a
                href={`https://steamcommunity.com/sharedfiles/filedetails/?id=${data.workshopId}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-sm text-primary hover:underline"
              >
                Workshop {data.workshopId}
                <ExternalLink className="size-3" />
              </a>
              {data.downloaded ? (
                <Badge variant="success">Downloaded</Badge>
              ) : (
                <Badge variant="secondary">Not downloaded</Badge>
              )}
              {data.updateAvailable && <Badge variant="warning">Update available</Badge>}
            </div>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button disabled={busy} onClick={() => download.mutate()}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : <DlIcon className="size-4" />}
            {dlLabel}
          </Button>
          <Button variant="destructive" onClick={() => setConfirmDelete(true)}>
            <Trash2 className="size-4" />
            Delete
          </Button>
        </div>
      </div>

      {item && (item.status === "downloading" || item.status === "error") && (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            <Progress
              value={item.percentage}
              variant={item.status === "error" ? "error" : "default"}
            />
            <p className="truncate text-xs text-muted-foreground" title={item.message}>
              {item.status === "error" ? item.message : `${item.message} ${item.fileName}`}
            </p>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Details</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 text-sm">
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Local path</span>
              <span className="truncate font-mono text-xs" title={data.localPath || undefined}>
                {data.localPath || "—"}
              </span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Last updated</span>
              <span>{formatDate(data.lastUpdated)}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-muted-foreground">Last checked</span>
              <span>{formatDate(data.lastChecked)}</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Usage</CardTitle>
            <CardDescription>
              Used by {data.servers.length} server{data.servers.length === 1 ? "" : "s"} and{" "}
              {data.modsets.length} modset{data.modsets.length === 1 ? "" : "s"}.
            </CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Deleting this mod removes it from the catalog and deletes downloaded files. Server
            assignments that reference it are removed as well.
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Servers ({data.servers.length})</CardTitle>
          <CardDescription>Servers that have this workshop mod assigned.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {data.servers.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.servers.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell>
                      <Link to={`/servers/${s.id}`} className="font-medium text-primary hover:underline">
                        {s.name}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <Badge variant={statusVariant(s.status)}>{s.status}</Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="p-6 text-sm text-muted-foreground">Not assigned to any server.</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Modsets ({data.modsets.length})</CardTitle>
          <CardDescription>Modsets that include this mod.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {data.modsets.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.modsets.map((ms) => (
                  <TableRow key={ms.id}>
                    <TableCell>
                      <Link
                        to={`/mods?modset=${ms.id}`}
                        className="font-medium text-primary hover:underline"
                      >
                        {ms.name}
                      </Link>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatDate(ms.createdAt)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="p-6 text-sm text-muted-foreground">Not in any modset.</p>
          )}
        </CardContent>
      </Card>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete mod?</DialogTitle>
            <DialogDescription>
              This removes “{data.name}” from the database and deletes its downloaded files.
              {serverNames.length > 0 && (
                <>
                  {" "}
                  It is currently assigned to{" "}
                  <strong>
                    {serverNames.length} server{serverNames.length === 1 ? "" : "s"}
                  </strong>
                  : {serverNames.join(", ")}. Those assignments will be removed.
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={remove.isPending}>
                Cancel
              </Button>
            </DialogClose>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              {remove.isPending && <Loader2 className="size-4 animate-spin" />}
              Delete permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
