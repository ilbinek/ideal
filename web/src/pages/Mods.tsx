import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Download,
  ExternalLink,
  FolderSync,
  Loader2,
  Plus,
  RefreshCw,
  RotateCw,
  Trash2,
} from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { formatDate } from "@/lib/utils";
import type { DownloadScope, Mod } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Select } from "@/components/ui/select";
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

function StatusCell({ mod }: { mod: Mod }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {mod.downloaded ? (
        <Badge variant="success">Downloaded</Badge>
      ) : (
        <Badge variant="secondary">Not downloaded</Badge>
      )}
      {mod.updateAvailable && <Badge variant="warning">Update available</Badge>}
    </div>
  );
}

function RowActions({ mod }: { mod: Mod }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { itemFor } = useDownloads();
  const item = itemFor(mod.id);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const download = useMutation({
    mutationFn: () => api.downloadMod(mod.id),
    onError: (e) =>
      toast.error("Download failed", e instanceof ApiError ? e.message : undefined),
  });

  const remove = useMutation({
    mutationFn: () => api.deleteMod(mod.id),
    onSuccess: () => {
      toast.success("Mod deleted");
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      setConfirmDelete(false);
    },
    onError: (e) =>
      toast.error("Delete failed", e instanceof ApiError ? e.message : undefined),
  });

  const busy =
    download.isPending || item?.status === "queued" || item?.status === "downloading";

  let label = mod.downloaded ? "Redownload" : "Download";
  let Icon = mod.downloaded ? RotateCw : Download;
  let variant: "default" | "outline" = "default";
  if (mod.downloaded && !mod.updateAvailable) variant = "outline";
  if (mod.updateAvailable) {
    label = "Update";
    Icon = RotateCw;
    variant = "default";
  }
  if (item?.status === "queued") label = "Queued";
  if (item?.status === "downloading") label = "Downloading";

  return (
    <div className="flex flex-col items-end gap-2">
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant={variant}
          disabled={busy}
          onClick={() => download.mutate()}
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
          {label}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => setConfirmDelete(true)}
          title="Delete mod"
        >
          <Trash2 className="size-4 text-destructive" />
        </Button>
      </div>

      {item && (item.status === "downloading" || item.status === "error") && (
        <div className="w-full min-w-48 max-w-72">
          <Progress
            value={item.percentage}
            variant={item.status === "error" ? "error" : "default"}
          />
          <p className="mt-1 truncate text-xs text-muted-foreground" title={item.message}>
            {item.status === "error" ? item.message : `${item.message} ${item.fileName}`}
          </p>
        </div>
      )}

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete mod?</DialogTitle>
            <DialogDescription>
              This removes “{mod.name}” from the database and deletes its downloaded files.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Cancel</Button>
            </DialogClose>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate()}
            >
              {remove.isPending && <Loader2 className="size-4 animate-spin" />}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function AddModDialog({ modset }: { modset?: number }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [workshopId, setWorkshopId] = useState("");
  const { data: modsets } = useQuery({ queryKey: ["modsets"], queryFn: () => api.getModsets() });
  const [targetModset, setTargetModset] = useState<string>(modset ? String(modset) : "");

  const add = useMutation({
    mutationFn: () =>
      api.addMod(workshopId.trim(), targetModset ? Number(targetModset) : null),
    onSuccess: (res) => {
      toast.success("Mod added", res.name);
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      setWorkshopId("");
      setOpen(false);
    },
    onError: (e) =>
      toast.error("Failed to add mod", e instanceof ApiError ? e.message : undefined),
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button variant="outline" onClick={() => setOpen(true)}>
        <Plus className="size-4" /> Add mod
      </Button>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add mod by Workshop ID</DialogTitle>
          <DialogDescription>
            The mod name is fetched from the Steam Workshop automatically.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            add.mutate();
          }}
          className="flex flex-col gap-4"
        >
          <div className="flex flex-col gap-2">
            <Label htmlFor="workshopId">Workshop ID</Label>
            <Input
              id="workshopId"
              value={workshopId}
              onChange={(e) => setWorkshopId(e.target.value)}
              placeholder="e.g. 450814997"
              autoFocus
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="targetModset">Add to modset (optional)</Label>
            <Select
              id="targetModset"
              value={targetModset}
              onChange={(e) => setTargetModset(e.target.value)}
            >
              <option value="">None (standalone mod)</option>
              {modsets?.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </Select>
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={add.isPending || !workshopId.trim()}>
              {add.isPending && <Loader2 className="size-4 animate-spin" />}
              Add mod
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function Mods() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const modsetParam = params.get("modset");
  const modset = modsetParam ? Number(modsetParam) : undefined;

  const { snapshot } = useDownloads();
  const { data: modsets } = useQuery({ queryKey: ["modsets"], queryFn: () => api.getModsets() });
  const { data: mods, isLoading } = useQuery({
    queryKey: ["mods", modset ?? null],
    queryFn: () => api.getMods(modset),
  });

  const counts = useMemo(() => {
    const list = mods ?? [];
    return {
      missing: list.filter((m) => !m.downloaded).length,
      outdated: list.filter((m) => m.downloaded && m.updateAvailable).length,
    };
  }, [mods]);

  const enqueue = useMutation({
    mutationFn: (scope: DownloadScope) => api.enqueue(scope, modset),
    onSuccess: (res, scope) => {
      if (res.enqueued === 0) toast.success("Nothing to download", `No ${scope} mods.`);
      else toast.success(`Queued ${res.enqueued} mod(s)`);
    },
    onError: (e) =>
      toast.error("Failed to queue", e instanceof ApiError ? e.message : undefined),
  });

  const checkUpdates = useMutation({
    mutationFn: () => api.checkUpdates(),
    onSuccess: (updates) => {
      const count = Object.values(updates).filter(Boolean).length;
      toast.success("Update check complete", `${count} update(s) available.`);
      queryClient.invalidateQueries({ queryKey: ["mods"] });
    },
    onError: (e) =>
      toast.error("Update check failed", e instanceof ApiError ? e.message : undefined),
  });

  const a3sync = useMutation({
    mutationFn: () => api.generateA3Sync(),
    onSuccess: () => toast.success("A3Sync folder generated"),
    onError: (e) =>
      toast.error("A3Sync failed", e instanceof ApiError ? e.message : undefined),
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Mods</h1>
          <p className="text-sm text-muted-foreground">
            Download, update and manage Workshop mods.
          </p>
        </div>
        <div className="w-56">
          <Select
            value={modsetParam ?? ""}
            onChange={(e) => {
              const v = e.target.value;
              setParams(v ? { modset: v } : {});
            }}
          >
            <option value="">All mods</option>
            {modsets?.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
          </Select>
        </div>
      </div>

      <Card>
        <CardContent className="flex flex-wrap items-center gap-2 p-4">
          <Button onClick={() => enqueue.mutate("all")} disabled={enqueue.isPending}>
            <Download className="size-4" /> Download all
          </Button>
          <Button
            variant="outline"
            onClick={() => enqueue.mutate("outdated")}
            disabled={enqueue.isPending || counts.outdated === 0}
          >
            <RotateCw className="size-4" /> Download outdated ({counts.outdated})
          </Button>
          <Button
            variant="outline"
            onClick={() => enqueue.mutate("missing")}
            disabled={enqueue.isPending || counts.missing === 0}
          >
            <Download className="size-4" /> Download missing ({counts.missing})
          </Button>
          <div className="mx-1 h-6 w-px bg-border" />
          <Button
            variant="outline"
            onClick={() => checkUpdates.mutate()}
            disabled={checkUpdates.isPending}
          >
            {checkUpdates.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <RefreshCw className="size-4" />
            )}
            Check for updates
          </Button>
          <Button variant="outline" onClick={() => a3sync.mutate()} disabled={a3sync.isPending}>
            {a3sync.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <FolderSync className="size-4" />
            )}
            Generate A3Sync
          </Button>
          <div className="ml-auto">
            <AddModDialog modset={modset} />
          </div>
        </CardContent>
      </Card>

      {snapshot.total > 0 && (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            <div className="flex items-center justify-between text-sm">
              <span className="font-medium">
                {snapshot.processing ? "Downloading queue" : "Queue finished"}
              </span>
              <span className="text-muted-foreground">
                {snapshot.done}/{snapshot.total}
              </span>
            </div>
            <Progress
              value={snapshot.total ? (snapshot.done / snapshot.total) * 100 : 0}
              variant={snapshot.processing ? "default" : "success"}
            />
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center justify-center p-10 text-muted-foreground">
              <Loader2 className="size-5 animate-spin" />
            </div>
          ) : mods && mods.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Workshop</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last updated</TableHead>
                  <TableHead>Last checked</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {mods.map((mod) => (
                  <TableRow
                    key={mod.id}
                    data-state={mod.updateAvailable ? "highlight" : undefined}
                  >
                    <TableCell className="font-medium">{mod.name}</TableCell>
                    <TableCell>
                      <a
                        href={`https://steamcommunity.com/sharedfiles/filedetails/?id=${mod.workshopId}`}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 text-primary hover:underline"
                      >
                        {mod.workshopId}
                        <ExternalLink className="size-3" />
                      </a>
                    </TableCell>
                    <TableCell>
                      <StatusCell mod={mod} />
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatDate(mod.lastUpdated)}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatDate(mod.lastChecked)}
                    </TableCell>
                    <TableCell>
                      <RowActions mod={mod} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="p-10 text-center text-sm text-muted-foreground">
              No mods found. Import a modset or add a mod by Workshop ID.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
