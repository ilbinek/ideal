import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Pencil, Terminal, Trash2, Upload } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { formatDate } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useToast } from "@/components/ui/toast";
import { PageHeader } from "@/components/PageHeader";
import type { Modset } from "@/lib/types";

function Checkbox({
  checked,
  onChange,
  "aria-label": ariaLabel,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  "aria-label": string;
}) {
  return (
    <input
      type="checkbox"
      className="size-4 shrink-0 cursor-pointer rounded border-border accent-primary"
      checked={checked}
      onChange={(e) => onChange(e.target.checked)}
      aria-label={ariaLabel}
    />
  );
}

export function Modsets() {
  const toast = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [pendingDelete, setPendingDelete] = useState<Modset | null>(null);
  const [pendingRename, setPendingRename] = useState<Modset | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [selectedModIds, setSelectedModIds] = useState<Set<number>>(new Set());

  const { data: modsets } = useQuery({
    queryKey: ["modsets"],
    queryFn: () => api.getModsets(),
  });

  const exclusiveMods = useQuery({
    queryKey: ["exclusive-mods", pendingDelete?.id],
    queryFn: () => api.getExclusiveMods(pendingDelete!.id),
    enabled: !!pendingDelete,
  });

  const selectedIds = useMemo(() => [...selectedModIds], [selectedModIds]);

  const selectedUsage = useQuery({
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
    enabled: !!pendingDelete && selectedIds.length > 0,
  });

  useEffect(() => {
    if (!pendingDelete) setSelectedModIds(new Set());
  }, [pendingDelete]);

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadModset(file),
    onSuccess: (res) => {
      toast.success("Modset imported", `${res.name} — ${res.count} mods`);
      queryClient.invalidateQueries({ queryKey: ["modsets"] });
      navigate(`/mods?modset=${res.modsetId}`);
    },
    onError: (e) =>
      toast.error("Import failed", e instanceof ApiError ? e.message : undefined),
  });

  const remove = useMutation({
    mutationFn: ({ id, deleteModIds }: { id: number; deleteModIds: number[] }) =>
      api.deleteModset(id, deleteModIds),
    onSuccess: (_res, vars) => {
      toast.success(
        vars.deleteModIds.length > 0
          ? `Modset deleted (${vars.deleteModIds.length} mod${vars.deleteModIds.length === 1 ? "" : "s"} removed)`
          : "Modset deleted",
      );
      queryClient.invalidateQueries({ queryKey: ["modsets"] });
      queryClient.invalidateQueries({ queryKey: ["mods"] });
      setPendingDelete(null);
    },
    onError: (e) =>
      toast.error("Delete failed", e instanceof ApiError ? e.message : undefined),
  });

  const rename = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) =>
      api.updateModset(id, name),
    onSuccess: () => {
      toast.success("Modset renamed");
      queryClient.invalidateQueries({ queryKey: ["modsets"] });
      setPendingRename(null);
    },
    onError: (e) =>
      toast.error("Rename failed", e instanceof ApiError ? e.message : undefined),
  });

  const trimmedRename = renameValue.trim();
  const canSaveRename =
    !!pendingRename &&
    trimmedRename !== "" &&
    trimmedRename !== pendingRename.name &&
    !rename.isPending;

  const exclusiveList = exclusiveMods.data ?? [];
  const allSelected =
    exclusiveList.length > 0 && exclusiveList.every((m) => selectedModIds.has(m.id));

  const toggleMod = (id: number, checked: boolean) => {
    setSelectedModIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Modsets"
        description="Import Arma 3 launcher preset HTML exports."
        actions={
          <>
            <input
              ref={fileInput}
              type="file"
              accept=".html,text/html"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) upload.mutate(file);
                e.target.value = "";
              }}
            />
            <Button onClick={() => fileInput.current?.click()} disabled={upload.isPending}>
              {upload.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Upload className="size-4" />
              )}
              Import preset
            </Button>
          </>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Your modsets</CardTitle>
          <CardDescription>
            Each modset is a named collection of Workshop mods.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {modsets && modsets.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {modsets.map((m) => (
                  <TableRow key={m.id}>
                    <TableCell className="font-medium">{m.name}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatDate(m.createdAt)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-2">
                        <Button asChild variant="outline" size="sm">
                          <Link to={`/mods?modset=${m.id}`}>View mods</Link>
                        </Button>
                        <Button asChild variant="outline" size="sm">
                          <Link to={`/startup-command?modset=${m.id}`}>
                            <Terminal className="size-4" /> Startup
                          </Link>
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => {
                            setPendingRename(m);
                            setRenameValue(m.name);
                          }}
                          title="Rename modset"
                        >
                          <Pencil className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => setPendingDelete(m)}
                          title="Delete modset"
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">
              No modsets yet. Import a launcher preset to get started.
            </p>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={!!pendingDelete}
        onOpenChange={(o) => {
          if (!o) setPendingDelete(null);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Delete modset?</DialogTitle>
            <DialogDescription>
              This removes the modset “{pendingDelete?.name}”.
              {exclusiveMods.isLoading ? (
                <> Loading exclusive mods…</>
              ) : exclusiveList.length > 0 ? (
                <>
                  {" "}
                  Only mods not used by other modsets are listed. Selected mods will be
                  permanently deleted (including downloaded files).
                </>
              ) : (
                <> Mods themselves are not deleted.</>
              )}
              {selectedIds.length > 0 &&
                (selectedUsage.isFetching ? (
                  <> Checking server usage…</>
                ) : selectedUsage.data && selectedUsage.data.modsInUse > 0 ? (
                  <>
                    {" "}
                    <strong>
                      {selectedUsage.data.modsInUse} of these mod
                      {selectedUsage.data.modsInUse === 1 ? " is" : "s are"}
                    </strong>{" "}
                    assigned to server
                    {selectedUsage.data.serverNames.length === 1 ? "" : "s"}:{" "}
                    {selectedUsage.data.serverNames.join(", ")}. Those assignments will be
                    removed.
                  </>
                ) : null)}
            </DialogDescription>
          </DialogHeader>

          {exclusiveMods.isLoading ? (
            <div className="flex items-center justify-center py-6 text-muted-foreground">
              <Loader2 className="size-5 animate-spin" />
            </div>
          ) : exclusiveList.length > 0 ? (
            <div className="flex flex-col gap-2 py-1">
              <div className="flex items-center justify-between gap-2">
                <p className="text-sm font-medium">
                  Exclusive mods ({selectedModIds.size} selected)
                </p>
                <div className="flex gap-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() =>
                      setSelectedModIds(new Set(exclusiveList.map((m) => m.id)))
                    }
                    disabled={allSelected}
                  >
                    Select all
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setSelectedModIds(new Set())}
                    disabled={selectedModIds.size === 0}
                  >
                    Clear
                  </Button>
                </div>
              </div>
              <div className="max-h-56 overflow-y-auto rounded-md border border-border">
                <ul className="divide-y divide-border">
                  {exclusiveList.map((m) => (
                    <li key={m.id}>
                      <label className="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted/50">
                        <Checkbox
                          checked={selectedModIds.has(m.id)}
                          onChange={(checked) => toggleMod(m.id, checked)}
                          aria-label={`Delete ${m.name}`}
                        />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">{m.name}</span>
                          {m.workshopId ? (
                            <span className="block truncate text-xs text-muted-foreground">
                              {m.workshopId}
                            </span>
                          ) : null}
                        </span>
                      </label>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          ) : exclusiveMods.isError ? (
            <p className="py-2 text-sm text-destructive">
              Failed to load exclusive mods. You can still delete the modset without
              removing mods.
            </p>
          ) : null}

          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" disabled={remove.isPending}>
                Cancel
              </Button>
            </DialogClose>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() =>
                pendingDelete &&
                remove.mutate({
                  id: pendingDelete.id,
                  deleteModIds: selectedIds,
                })
              }
            >
              {remove.isPending && <Loader2 className="size-4 animate-spin" />}
              {selectedIds.length > 0
                ? `Delete modset + ${selectedIds.length} mod${selectedIds.length === 1 ? "" : "s"}`
                : "Delete modset"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!pendingRename}
        onOpenChange={(o) => {
          if (!o) setPendingRename(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename modset</DialogTitle>
            <DialogDescription>
              Choose a new name for “{pendingRename?.name}”.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2 py-2">
            <Label htmlFor="modset-rename">Name</Label>
            <Input
              id="modset-rename"
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && canSaveRename && pendingRename) {
                  rename.mutate({ id: pendingRename.id, name: trimmedRename });
                }
              }}
              autoFocus
            />
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Cancel</Button>
            </DialogClose>
            <Button
              disabled={!canSaveRename}
              onClick={() =>
                pendingRename &&
                rename.mutate({ id: pendingRename.id, name: trimmedRename })
              }
            >
              {rename.isPending && <Loader2 className="size-4 animate-spin" />}
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
