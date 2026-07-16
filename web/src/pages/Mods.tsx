import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Download,
  ExternalLink,
  FolderSync,
  Loader2,
  Plus,
  RefreshCw,
  RotateCw,
  Search,
  Trash2,
  Unlink,
  X,
} from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { cn, formatDate } from "@/lib/utils";
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
import { PageHeader } from "@/components/PageHeader";

type StatusFilter = "all" | "downloaded" | "missing" | "outdated";
type SortKey = "name" | "workshopId" | "status" | "lastUpdated" | "lastChecked";
type SortDir = "asc" | "desc";

const STATUS_FILTERS: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "downloaded", label: "Downloaded" },
  { value: "missing", label: "Missing" },
  { value: "outdated", label: "Update available" },
];

function statusRank(mod: Mod): number {
  if (mod.updateAvailable) return 2;
  if (!mod.downloaded) return 1;
  return 0;
}

function compareMods(a: Mod, b: Mod, key: SortKey, dir: SortDir): number {
  const mul = dir === "asc" ? 1 : -1;
  let cmp = 0;
  switch (key) {
    case "name":
      cmp = a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
      break;
    case "workshopId":
      cmp = a.workshopId.localeCompare(b.workshopId, undefined, { numeric: true });
      break;
    case "status":
      cmp = statusRank(a) - statusRank(b);
      break;
    case "lastUpdated":
      cmp = (a.lastUpdated || "").localeCompare(b.lastUpdated || "");
      break;
    case "lastChecked":
      cmp = (a.lastChecked || "").localeCompare(b.lastChecked || "");
      break;
  }
  return cmp * mul;
}

function Checkbox({
  checked,
  indeterminate,
  onChange,
  "aria-label": ariaLabel,
}: {
  checked: boolean;
  indeterminate?: boolean;
  onChange: (checked: boolean) => void;
  "aria-label": string;
}) {
  return (
    <input
      type="checkbox"
      className="size-4 cursor-pointer rounded border-border accent-primary"
      checked={checked}
      ref={(el) => {
        if (el) el.indeterminate = !!indeterminate && !checked;
      }}
      onChange={(e) => onChange(e.target.checked)}
      aria-label={ariaLabel}
      onClick={(e) => e.stopPropagation()}
    />
  );
}

function SortableHead({
  label,
  sortKey,
  currentKey,
  dir,
  onSort,
  className,
}: {
  label: string;
  sortKey: SortKey;
  currentKey: SortKey;
  dir: SortDir;
  onSort: (key: SortKey) => void;
  className?: string;
}) {
  const active = currentKey === sortKey;
  const Icon = !active ? ArrowUpDown : dir === "asc" ? ArrowUp : ArrowDown;
  return (
    <TableHead className={className}>
      <button
        type="button"
        className={cn(
          "inline-flex items-center gap-1.5 font-medium hover:text-foreground",
          active ? "text-foreground" : "text-muted-foreground",
        )}
        onClick={() => onSort(sortKey)}
      >
        {label}
        <Icon className="size-3.5" />
      </button>
    </TableHead>
  );
}

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

function ServerUsageWarning({ servers }: { servers: { name: string }[] }) {
  if (servers.length === 0) return null;
  const names = servers.map((s) => s.name);
  return (
    <>
      {" "}
      It is assigned to{" "}
      <strong>
        {names.length} server{names.length === 1 ? "" : "s"}
      </strong>
      : {names.join(", ")}. Those assignments will be removed.
    </>
  );
}

function RowActions({
  mod,
  modset,
}: {
  mod: Mod;
  modset?: number;
}) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { itemFor } = useDownloads();
  const item = itemFor(mod.id);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const usage = useQuery({
    queryKey: ["mods", mod.id],
    queryFn: () => api.getMod(mod.id),
    enabled: confirmOpen,
  });

  const download = useMutation({
    mutationFn: () => api.downloadMod(mod.id),
    onError: (e) =>
      toast.error("Download failed", e instanceof ApiError ? e.message : undefined),
  });

  const removePermanent = useMutation({
    mutationFn: () => api.deleteMod(mod.id),
    onSuccess: () => {
      toast.success("Mod deleted");
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      setConfirmOpen(false);
    },
    onError: (e) =>
      toast.error("Delete failed", e instanceof ApiError ? e.message : undefined),
  });

  const unlink = useMutation({
    mutationFn: () => api.removeModFromModset(modset!, mod.id),
    onSuccess: () => {
      toast.success("Removed from modset");
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      setConfirmOpen(false);
    },
    onError: (e) =>
      toast.error("Remove failed", e instanceof ApiError ? e.message : undefined),
  });

  const busy =
    download.isPending || item?.status === "queued" || item?.status === "downloading";
  const actionPending = removePermanent.isPending || unlink.isPending;
  const servers = usage.data?.servers ?? [];

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
          onClick={() => setConfirmOpen(true)}
          title={modset ? "Remove or delete" : "Delete mod"}
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

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{modset ? "Remove mod?" : "Delete mod?"}</DialogTitle>
            <DialogDescription>
              {modset ? (
                <>
                  Choose how to handle “{mod.name}” in this modset.
                  {usage.isFetching ? (
                    <> Checking server usage…</>
                  ) : (
                    <ServerUsageWarning servers={servers} />
                  )}
                </>
              ) : (
                <>
                  This removes “{mod.name}” from the database and deletes its downloaded files.
                  {usage.isFetching ? (
                    <> Checking server usage…</>
                  ) : (
                    <ServerUsageWarning servers={servers} />
                  )}
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="flex-col gap-2 sm:flex-row sm:justify-end">
            <DialogClose asChild>
              <Button variant="outline" disabled={actionPending}>
                Cancel
              </Button>
            </DialogClose>
            {modset ? (
              <>
                <Button
                  variant="outline"
                  disabled={actionPending}
                  onClick={() => unlink.mutate()}
                >
                  {unlink.isPending && <Loader2 className="size-4 animate-spin" />}
                  <Unlink className="size-4" />
                  Remove from modset
                </Button>
                <Button
                  variant="destructive"
                  disabled={actionPending}
                  onClick={() => removePermanent.mutate()}
                >
                  {removePermanent.isPending && <Loader2 className="size-4 animate-spin" />}
                  Delete permanently
                </Button>
              </>
            ) : (
              <Button
                variant="destructive"
                disabled={actionPending}
                onClick={() => removePermanent.mutate()}
              >
                {removePermanent.isPending && <Loader2 className="size-4 animate-spin" />}
                Delete
              </Button>
            )}
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

function parseStatusFilter(raw: string | null): StatusFilter {
  if (raw === "downloaded" || raw === "missing" || raw === "outdated") return raw;
  return "all";
}

function parseSortKey(raw: string | null): SortKey {
  if (
    raw === "name" ||
    raw === "workshopId" ||
    raw === "status" ||
    raw === "lastUpdated" ||
    raw === "lastChecked"
  ) {
    return raw;
  }
  return "name";
}

function parseSortDir(raw: string | null): SortDir {
  return raw === "desc" ? "desc" : "asc";
}

export function Mods() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const modsetParam = params.get("modset");
  const modset = modsetParam ? Number(modsetParam) : undefined;

  const search = params.get("q") ?? "";
  const statusFilter = parseStatusFilter(params.get("status"));
  const sortKey = parseSortKey(params.get("sort"));
  const sortDir = parseSortDir(params.get("dir"));

  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);
  const [bulkUnlinkOpen, setBulkUnlinkOpen] = useState(false);

  const { snapshot } = useDownloads();
  const { data: modsets } = useQuery({ queryKey: ["modsets"], queryFn: () => api.getModsets() });
  const { data: mods, isLoading } = useQuery({
    queryKey: ["mods", modset ?? null],
    queryFn: () => api.getMods(modset),
  });

  const selectedIds = useMemo(() => [...selected], [selected]);
  const bulkUsage = useQuery({
    queryKey: ["mods-usage", selectedIds],
    queryFn: async () => {
      const details = await Promise.all(selectedIds.map((id) => api.getMod(id)));
      const serverNames = new Set<string>();
      let modsInUse = 0;
      for (const d of details) {
        if (d.servers.length > 0) {
          modsInUse++;
          for (const s of d.servers) serverNames.add(s.name);
        }
      }
      return { modsInUse, serverNames: [...serverNames] };
    },
    enabled: bulkDeleteOpen && selectedIds.length > 0,
  });

  const updateParams = (patch: Record<string, string | null>) => {
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      for (const [key, value] of Object.entries(patch)) {
        if (value === null || value === "") next.delete(key);
        else next.set(key, value);
      }
      return next;
    }, { replace: true });
  };

  const setModsetFilter = (value: string) => {
    setSelected(new Set());
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      if (value) next.set("modset", value);
      else next.delete("modset");
      return next;
    }, { replace: true });
  };

  const counts = useMemo(() => {
    const list = mods ?? [];
    return {
      all: list.length,
      downloaded: list.filter((m) => m.downloaded).length,
      missing: list.filter((m) => !m.downloaded).length,
      outdated: list.filter((m) => m.updateAvailable).length,
    };
  }, [mods]);

  const visibleMods = useMemo(() => {
    const q = search.trim().toLowerCase();
    let list = mods ?? [];

    if (q) {
      list = list.filter(
        (m) =>
          m.name.toLowerCase().includes(q) || m.workshopId.toLowerCase().includes(q),
      );
    }

    switch (statusFilter) {
      case "downloaded":
        list = list.filter((m) => m.downloaded);
        break;
      case "missing":
        list = list.filter((m) => !m.downloaded);
        break;
      case "outdated":
        list = list.filter((m) => m.updateAvailable);
        break;
    }

    return [...list].sort((a, b) => compareMods(a, b, sortKey, sortDir));
  }, [mods, search, statusFilter, sortKey, sortDir]);

  // Drop selections that are no longer visible.
  useEffect(() => {
    const visible = new Set(visibleMods.map((m) => m.id));
    setSelected((prev) => {
      let changed = false;
      const next = new Set<number>();
      for (const id of prev) {
        if (visible.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [visibleMods]);

  const allVisibleSelected =
    visibleMods.length > 0 && visibleMods.every((m) => selected.has(m.id));
  const someVisibleSelected =
    visibleMods.some((m) => selected.has(m.id)) && !allVisibleSelected;

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      updateParams({ dir: sortDir === "asc" ? "desc" : "asc" });
    } else {
      updateParams({ sort: key, dir: "asc" });
    }
  };

  const toggleOne = (id: number, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const toggleAllVisible = (checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      for (const m of visibleMods) {
        if (checked) next.add(m.id);
        else next.delete(m.id);
      }
      return next;
    });
  };

  const clearSelection = () => setSelected(new Set());

  const enqueue = useMutation({
    mutationFn: (scope: DownloadScope) => api.enqueue(scope, modset),
    onSuccess: (res, scope) => {
      if (res.enqueued === 0) toast.success("Nothing to download", `No ${scope} mods.`);
      else toast.success(`Queued ${res.enqueued} mod(s)`);
    },
    onError: (e) =>
      toast.error("Failed to queue", e instanceof ApiError ? e.message : undefined),
  });

  const enqueueSelected = useMutation({
    mutationFn: () => api.enqueueIds([...selected]),
    onSuccess: (res) => {
      if (res.enqueued === 0) toast.success("Nothing to download", "Selected mods already queued.");
      else toast.success(`Queued ${res.enqueued} mod(s)`);
      clearSelection();
    },
    onError: (e) =>
      toast.error("Failed to queue", e instanceof ApiError ? e.message : undefined),
  });

  const checkUpdates = useMutation({
    mutationFn: () => api.checkUpdates(modset),
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

  const bulkDelete = useMutation({
    mutationFn: () => api.bulkDeleteMods([...selected]),
    onSuccess: (res) => {
      if (res.failed.length === 0) {
        toast.success(`Deleted ${res.deleted} mod(s)`);
      } else {
        toast.error(
          `Deleted ${res.deleted}, failed ${res.failed.length}`,
          `Could not delete: ${res.failed.join(", ")}`,
        );
      }
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      clearSelection();
      setBulkDeleteOpen(false);
    },
    onError: (e) =>
      toast.error("Bulk delete failed", e instanceof ApiError ? e.message : undefined),
  });

  const bulkUnlink = useMutation({
    mutationFn: async () => {
      if (!modset) return { ok: 0, failed: 0 };
      let ok = 0;
      let failed = 0;
      for (const id of selected) {
        try {
          await api.removeModFromModset(modset, id);
          ok++;
        } catch {
          failed++;
        }
      }
      return { ok, failed };
    },
    onSuccess: (res) => {
      if (res.failed === 0) toast.success(`Removed ${res.ok} mod(s) from modset`);
      else toast.error(`Removed ${res.ok}, failed ${res.failed}`);
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      clearSelection();
      setBulkUnlinkOpen(false);
    },
    onError: (e) =>
      toast.error("Remove failed", e instanceof ApiError ? e.message : undefined),
  });

  const selectedCount = selected.size;
  const hasMods = (mods?.length ?? 0) > 0;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Mods"
        description="Download, update and manage Workshop mods."
        actions={
          <div className="w-56">
            <Select value={modsetParam ?? ""} onChange={(e) => setModsetFilter(e.target.value)}>
              <option value="">All mods</option>
              {modsets?.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </Select>
          </div>
        }
      />

      <Card>
        <CardContent className="flex flex-wrap items-center gap-2 p-4 sm:p-5">
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
          <CardContent className="flex flex-col gap-2 p-4 sm:p-5">
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
        <CardContent className="flex flex-col gap-3 p-4 sm:p-5">
          <div className="relative max-w-md">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => updateParams({ q: e.target.value || null })}
              placeholder="Search by name or Workshop ID…"
              className="pl-9"
            />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {STATUS_FILTERS.map((f) => {
              const count =
                f.value === "all"
                  ? counts.all
                  : f.value === "downloaded"
                    ? counts.downloaded
                    : f.value === "missing"
                      ? counts.missing
                      : counts.outdated;
              const active = statusFilter === f.value;
              return (
                <Button
                  key={f.value}
                  size="sm"
                  variant={active ? "default" : "outline"}
                  onClick={() =>
                    updateParams({ status: f.value === "all" ? null : f.value })
                  }
                >
                  {f.label} ({count})
                </Button>
              );
            })}
            <span className="ml-auto text-sm text-muted-foreground">
              Showing {visibleMods.length}
              {hasMods ? ` of ${mods!.length}` : ""}
            </span>
          </div>
        </CardContent>
      </Card>

      {selectedCount > 0 && (
        <Card>
          <CardContent className="flex flex-wrap items-center gap-2 p-4 sm:p-5">
            <span className="text-sm font-medium">{selectedCount} selected</span>
            <Button
              size="sm"
              onClick={() => enqueueSelected.mutate()}
              disabled={enqueueSelected.isPending}
            >
              {enqueueSelected.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Download className="size-4" />
              )}
              Download selected
            </Button>
            {modset && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => setBulkUnlinkOpen(true)}
                disabled={bulkUnlink.isPending}
              >
                <Unlink className="size-4" />
                Remove from modset
              </Button>
            )}
            <Button
              size="sm"
              variant="destructive"
              onClick={() => setBulkDeleteOpen(true)}
              disabled={bulkDelete.isPending}
            >
              <Trash2 className="size-4" />
              Delete permanently
            </Button>
            <Button size="sm" variant="ghost" onClick={clearSelection}>
              <X className="size-4" />
              Clear
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center justify-center p-10 text-muted-foreground">
              <Loader2 className="size-5 animate-spin" />
            </div>
          ) : hasMods ? (
            visibleMods.length > 0 ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-10">
                      <Checkbox
                        checked={allVisibleSelected}
                        indeterminate={someVisibleSelected}
                        onChange={toggleAllVisible}
                        aria-label="Select all visible mods"
                      />
                    </TableHead>
                    <SortableHead
                      label="Name"
                      sortKey="name"
                      currentKey={sortKey}
                      dir={sortDir}
                      onSort={toggleSort}
                    />
                    <SortableHead
                      label="Workshop"
                      sortKey="workshopId"
                      currentKey={sortKey}
                      dir={sortDir}
                      onSort={toggleSort}
                    />
                    <SortableHead
                      label="Status"
                      sortKey="status"
                      currentKey={sortKey}
                      dir={sortDir}
                      onSort={toggleSort}
                    />
                    <SortableHead
                      label="Last updated"
                      sortKey="lastUpdated"
                      currentKey={sortKey}
                      dir={sortDir}
                      onSort={toggleSort}
                    />
                    <SortableHead
                      label="Last checked"
                      sortKey="lastChecked"
                      currentKey={sortKey}
                      dir={sortDir}
                      onSort={toggleSort}
                    />
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleMods.map((mod) => (
                    <TableRow
                      key={mod.id}
                      data-state={
                        selected.has(mod.id)
                          ? "selected"
                          : mod.updateAvailable
                            ? "highlight"
                            : undefined
                      }
                    >
                      <TableCell>
                        <Checkbox
                          checked={selected.has(mod.id)}
                          onChange={(checked) => toggleOne(mod.id, checked)}
                          aria-label={`Select ${mod.name}`}
                        />
                      </TableCell>
                      <TableCell className="font-medium">
                        <Link
                          to={`/mods/${mod.id}`}
                          className="text-primary hover:underline"
                        >
                          {mod.name}
                        </Link>
                      </TableCell>
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
                        <RowActions mod={mod} modset={modset} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <p className="p-10 text-center text-sm text-muted-foreground">
                No mods match the current search or filters.
              </p>
            )
          ) : (
            <p className="p-10 text-center text-sm text-muted-foreground">
              No mods found. Import a modset or add a mod by Workshop ID.
            </p>
          )}
        </CardContent>
      </Card>

      <Dialog open={bulkDeleteOpen} onOpenChange={setBulkDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {selectedCount} mod(s)?</DialogTitle>
            <DialogDescription>
              This permanently removes the selected mods from the database and deletes their
              downloaded files. This cannot be undone.
              {bulkUsage.isFetching ? (
                <> Checking server usage…</>
              ) : bulkUsage.data && bulkUsage.data.modsInUse > 0 ? (
                <>
                  {" "}
                  <strong>
                    {bulkUsage.data.modsInUse} of these mod
                    {bulkUsage.data.modsInUse === 1 ? " is" : "s are"}
                  </strong>{" "}
                  assigned to server
                  {bulkUsage.data.serverNames.length === 1 ? "" : "s"}:{" "}
                  {bulkUsage.data.serverNames.join(", ")}. Those assignments will be removed.
                </>
              ) : null}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={bulkDelete.isPending}>
                Cancel
              </Button>
            </DialogClose>
            <Button
              variant="destructive"
              disabled={bulkDelete.isPending}
              onClick={() => bulkDelete.mutate()}
            >
              {bulkDelete.isPending && <Loader2 className="size-4 animate-spin" />}
              Delete permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={bulkUnlinkOpen} onOpenChange={setBulkUnlinkOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove {selectedCount} mod(s) from modset?</DialogTitle>
            <DialogDescription>
              Mods stay in the global catalog and downloaded files are kept. Only the link to
              this modset is removed.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={bulkUnlink.isPending}>
                Cancel
              </Button>
            </DialogClose>
            <Button disabled={bulkUnlink.isPending} onClick={() => bulkUnlink.mutate()}>
              {bulkUnlink.isPending && <Loader2 className="size-4 animate-spin" />}
              <Unlink className="size-4" />
              Remove from modset
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
