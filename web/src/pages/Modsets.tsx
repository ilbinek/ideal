import { useRef, useState } from "react";
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

export function Modsets() {
  const toast = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [pendingDelete, setPendingDelete] = useState<Modset | null>(null);
  const [pendingRename, setPendingRename] = useState<Modset | null>(null);
  const [renameValue, setRenameValue] = useState("");

  const { data: modsets } = useQuery({
    queryKey: ["modsets"],
    queryFn: () => api.getModsets(),
  });

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
    mutationFn: (id: number) => api.deleteModset(id),
    onSuccess: () => {
      toast.success("Modset deleted");
      queryClient.invalidateQueries({ queryKey: ["modsets"] });
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

      <Dialog open={!!pendingDelete} onOpenChange={(o) => !o && setPendingDelete(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete modset?</DialogTitle>
            <DialogDescription>
              This removes the modset “{pendingDelete?.name}”. Mods themselves are not
              deleted.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Cancel</Button>
            </DialogClose>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => pendingDelete && remove.mutate(pendingDelete.id)}
            >
              {remove.isPending && <Loader2 className="size-4 animate-spin" />}
              Delete
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
