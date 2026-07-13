import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Loader2, Plus, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "@/lib/api";
import { useBranchInstall } from "@/hooks/useServerLogs";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useState } from "react";
import * as React from "react";

export function Branches() {
  const queryClient = useQueryClient();
  const installSnap = useBranchInstall();
  const [newName, setNewName] = useState("");
  const [newSteamBranch, setNewSteamBranch] = useState("");

  const branches = useQuery({
    queryKey: ["branches"],
    queryFn: () => api.getBranches(),
  });

  const install = useMutation({
    mutationFn: (id: number) => api.installBranch(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["branches"] }),
  });

  // Refresh branches when install completes
  const prevInstalling = React.useRef(installSnap.installing);
  React.useEffect(() => {
    if (prevInstalling.current && !installSnap.installing && !installSnap.error) {
      queryClient.invalidateQueries({ queryKey: ["branches"] });
    }
    prevInstalling.current = installSnap.installing;
  }, [installSnap.installing, installSnap.error, queryClient]);

  const create = useMutation({
    mutationFn: () => api.createBranch(newName, newSteamBranch),
    onSuccess: () => {
      setNewName("");
      setNewSteamBranch("");
      queryClient.invalidateQueries({ queryKey: ["branches"] });
    },
  });

  const remove = useMutation({
    mutationFn: (id: number) => api.deleteBranch(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["branches"] }),
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Branches</h1>
          <p className="text-sm text-muted-foreground">
            Central Arma 3 dedicated server installs via SteamCMD.
          </p>
        </div>
        <Button variant="outline" asChild>
          <Link to="/servers">Back to servers</Link>
        </Button>
      </div>

      {installSnap.installing && (
        <Card>
          <CardContent className="flex flex-col gap-2 pt-6">
            <p className="text-sm">{installSnap.message}</p>
            <Progress value={installSnap.percentage} />
            {installSnap.error && (
              <p className="text-sm text-destructive">{installSnap.error}</p>
            )}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Installed branches</CardTitle>
        </CardHeader>
        <CardContent>
          {branches.isLoading ? (
            <Loader2 className="size-5 animate-spin" />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Steam branch</TableHead>
                  <TableHead>Install path</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {branches.data?.map((b) => (
                  <TableRow key={b.id}>
                    <TableCell className="font-medium">{b.name}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {b.steamBranch || "(default)"}
                    </TableCell>
                    <TableCell className="max-w-[200px] truncate text-xs text-muted-foreground" title={b.installPath}>
                      {b.installPath}
                    </TableCell>
                    <TableCell>
                      <Badge variant={b.installed ? "default" : "outline"}>
                        {b.installed ? "installed" : "not installed"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={install.isPending || installSnap.installing}
                          onClick={() => install.mutate(b.id)}
                        >
                          <Download className="size-3.5" />
                          {b.installed ? "Update" : "Install"}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={remove.isPending}
                          onClick={() => {
                            if (
                              confirm(
                                `Delete branch "${b.name}"? This removes the database entry and deletes files at ${b.installPath}`,
                              )
                            ) {
                              remove.mutate(b.id);
                            }
                          }}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Add branch</CardTitle>
          <CardDescription>
            Steam beta name (e.g. profiling, creativedev, creatordlc). Leave empty for main.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex flex-1 flex-col gap-2">
            <Label>Display name</Label>
            <Input value={newName} onChange={(e) => setNewName(e.target.value)} />
          </div>
          <div className="flex flex-1 flex-col gap-2">
            <Label>Steam branch</Label>
            <Input
              value={newSteamBranch}
              onChange={(e) => setNewSteamBranch(e.target.value)}
              placeholder="optional"
            />
          </div>
          <Button disabled={!newName || create.isPending} onClick={() => create.mutate()}>
            <Plus className="size-4" /> Add
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
