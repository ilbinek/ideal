import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowLeft, Loader2 } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select } from "@/components/ui/select";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function ServerCreate() {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [branchId, setBranchId] = useState("");
  const [modsetId, setModsetId] = useState("none");
  const [gamePort, setGamePort] = useState("2302");
  const [steamPort, setSteamPort] = useState("2303");
  const [profileName, setProfileName] = useState("Server");
  const [error, setError] = useState("");

  const branches = useQuery({ queryKey: ["branches"], queryFn: () => api.getBranches() });
  const modsets = useQuery({ queryKey: ["modsets"], queryFn: () => api.getModsets() });

  const create = useMutation({
    mutationFn: () =>
      api.createServer({
        name,
        branchId: parseInt(branchId, 10),
        modsetId: modsetId && modsetId !== "none" ? parseInt(modsetId, 10) : null,
        gamePort: parseInt(gamePort, 10) || 2302,
        steamPort: parseInt(steamPort, 10) || 2303,
        profileName,
      }),
    onSuccess: (data) => navigate(`/servers/${data.id}`),
    onError: (e: Error) => setError(e.message),
  });

  return (
    <div className="mx-auto flex max-w-lg flex-col gap-6">
      <Button variant="ghost" size="sm" className="w-fit" asChild>
        <Link to="/servers">
          <ArrowLeft className="size-4" /> Back
        </Link>
      </Button>

      <Card>
        <CardHeader>
          <CardTitle>Create server</CardTitle>
          <CardDescription>
            A new instance folder will be provisioned with symlinks to the selected branch.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="name">Name</Label>
            <Input id="name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>

          <div className="flex flex-col gap-2">
            <Label>Branch</Label>
            <Select value={branchId} onChange={(e) => setBranchId(e.target.value)}>
              <option value="">Select branch</option>
              {branches.data?.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name}
                  {!b.installed ? " (not installed)" : ""}
                </option>
              ))}
            </Select>
          </div>

          <div className="flex flex-col gap-2">
            <Label>Import modset (optional)</Label>
            <Select value={modsetId} onChange={(e) => setModsetId(e.target.value)}>
              <option value="none">None</option>
              {modsets.data?.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </Select>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="gamePort">Game port</Label>
              <Input
                id="gamePort"
                type="number"
                value={gamePort}
                onChange={(e) => setGamePort(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="steamPort">Steam port</Label>
              <Input
                id="steamPort"
                type="number"
                value={steamPort}
                onChange={(e) => setSteamPort(e.target.value)}
              />
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="profile">Profile name</Label>
            <Input
              id="profile"
              value={profileName}
              onChange={(e) => setProfileName(e.target.value)}
            />
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          <Button
            disabled={!name || !branchId || create.isPending}
            onClick={() => create.mutate()}
          >
            {create.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              "Create server"
            )}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
