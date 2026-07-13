import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Copy,
  Loader2,
  Play,
  RefreshCw,
  RotateCcw,
  Square,
  Trash2,
  Upload,
} from "lucide-react";
import { api } from "@/lib/api";
import { patchServerStatus, useServers } from "@/hooks/useServers";
import { useServerLogs } from "@/hooks/useServerLogs";
import { formatDate } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import type { ServerMod } from "@/lib/types";

type Tab = "overview" | "mods" | "keys" | "config" | "logs" | "missions";

export function ServerDetail() {
  const { id } = useParams<{ id: string }>();
  const serverId = parseInt(id ?? "0", 10);
  const queryClient = useQueryClient();
  const { statusFor } = useServers();
  const [tab, setTab] = useState<Tab>("overview");

  const server = useQuery({
    queryKey: ["servers", serverId],
    queryFn: () => api.getServer(serverId),
    enabled: serverId > 0,
    refetchInterval: (query) => {
      const s = query.state.data?.status;
      return s === "starting" || s === "stopping" ? 2000 : false;
    },
  });

  const live = statusFor(serverId);
  const status = server.data?.status ?? live?.status ?? "stopped";
  const isRunning = status === "running" || status === "starting";

  const start = useMutation({
    mutationFn: () => api.startServer(serverId),
    onMutate: () => patchServerStatus(queryClient, serverId, "starting"),
    onSuccess: () => {
      patchServerStatus(queryClient, serverId, "running");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
    onError: () => {
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
    },
  });
  const stop = useMutation({
    mutationFn: () => api.stopServer(serverId),
    onMutate: () => patchServerStatus(queryClient, serverId, "stopping"),
    onSuccess: () => {
      patchServerStatus(queryClient, serverId, "stopped");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
    onError: () => {
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
    },
  });
  const restart = useMutation({
    mutationFn: () => api.restartServer(serverId),
    onMutate: () => patchServerStatus(queryClient, serverId, "starting"),
    onSuccess: () => {
      patchServerStatus(queryClient, serverId, "running");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
    onError: () => {
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
    },
  });
  const del = useMutation({
    mutationFn: () => api.deleteServer(serverId),
    onSuccess: () => {
      window.location.href = "/servers";
    },
  });

  const tabs: { id: Tab; label: string }[] = [
    { id: "overview", label: "Overview" },
    { id: "mods", label: "Mods" },
    { id: "keys", label: "Keys" },
    { id: "config", label: "Config" },
    { id: "logs", label: "Logs" },
    { id: "missions", label: "Missions" },
  ];

  if (server.isLoading) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 className="size-6 animate-spin" />
      </div>
    );
  }

  if (!server.data) {
    return <p className="text-muted-foreground">Server not found.</p>;
  }

  const srv = server.data;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-4">
        <Button variant="ghost" size="sm" asChild>
          <Link to="/servers">
            <ArrowLeft className="size-4" /> Servers
          </Link>
        </Button>
        <div className="flex-1">
          <h1 className="text-2xl font-semibold">{srv.name}</h1>
          <p className="text-sm text-muted-foreground">
            :{srv.gamePort} / steam :{srv.steamPort} · {srv.slug}
          </p>
        </div>
        <Badge>{status}</Badge>
        <div className="flex gap-2">
          <Button
            size="sm"
            disabled={start.isPending || stop.isPending}
            onClick={() => (isRunning ? stop.mutate() : start.mutate())}
          >
            {isRunning ? <Square className="size-3.5" /> : <Play className="size-3.5" />}
            {isRunning ? "Stop" : "Start"}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={restart.isPending}
            onClick={() => restart.mutate()}
          >
            <RotateCcw className="size-3.5" /> Restart
          </Button>
          <Button
            size="sm"
            variant="destructive"
            disabled={isRunning || del.isPending}
            onClick={() => {
              if (confirm("Delete this server and its instance folder?")) del.mutate();
            }}
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap gap-1 border-b border-border pb-1">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
              tab === t.id
                ? "bg-secondary text-foreground"
                : "text-muted-foreground hover:bg-accent"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "overview" && <OverviewTab serverId={serverId} srv={srv} />}
      {tab === "mods" && <ModsTab serverId={serverId} />}
      {tab === "keys" && <KeysTab serverId={serverId} />}
      {tab === "config" && <ConfigTab serverId={serverId} serverStatus={status} />}
      {tab === "logs" && <LogsTab serverId={serverId} isRunning={isRunning} />}
      {tab === "missions" && <MissionsTab serverId={serverId} />}
    </div>
  );
}

function OverviewTab({
  serverId,
  srv,
}: {
  serverId: number;
  srv: {
    name: string;
    branchId: number;
    modsetId?: number;
    gamePort: number;
    steamPort: number;
    profileName: string;
    serverBinary: string;
    extraLaunchParams: string;
  };
}) {
  const queryClient = useQueryClient();
  const branches = useQuery({ queryKey: ["branches"], queryFn: () => api.getBranches() });
  const modsets = useQuery({ queryKey: ["modsets"], queryFn: () => api.getModsets() });
  const launch = useQuery({
    queryKey: ["launch", serverId],
    queryFn: () => api.getLaunchPreview(serverId),
  });

  const [name, setName] = useState(srv.name);
  const [branchId, setBranchId] = useState(String(srv.branchId));
  const [modsetId, setModsetId] = useState(srv.modsetId ? String(srv.modsetId) : "none");
  const [gamePort, setGamePort] = useState(String(srv.gamePort));
  const [steamPort, setSteamPort] = useState(String(srv.steamPort));
  const [profileName, setProfileName] = useState(srv.profileName);
  const [serverBinary, setServerBinary] = useState(srv.serverBinary ?? "");
  const [extra, setExtra] = useState(srv.extraLaunchParams);

  const branchIdNum = parseInt(branchId, 10);
  const binaries = useQuery({
    queryKey: ["branchBinaries", branchIdNum],
    queryFn: () => api.getBranchBinaries(branchIdNum),
    enabled: Number.isFinite(branchIdNum) && branchIdNum > 0,
  });

  useEffect(() => {
    setModsetId(srv.modsetId ? String(srv.modsetId) : "none");
  }, [srv.modsetId]);

  useEffect(() => {
    setServerBinary(srv.serverBinary ?? "");
  }, [srv.serverBinary]);

  const save = useMutation({
    mutationFn: () =>
      api.updateServer(serverId, {
        name,
        branchId: parseInt(branchId, 10),
        modsetId: modsetId !== "none" ? parseInt(modsetId, 10) : null,
        gamePort: parseInt(gamePort, 10),
        steamPort: parseInt(steamPort, 10),
        profileName,
        serverBinary,
        extraLaunchParams: extra,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["serverMods", serverId] });
      queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] });
      queryClient.invalidateQueries({ queryKey: ["launch", serverId] });
    },
  });

  const provision = useMutation({
    mutationFn: () => api.provisionServer(serverId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] });
      queryClient.invalidateQueries({ queryKey: ["launch", serverId] });
    },
  });

  const binaryOptions = binaries.data?.binaries ?? [];
  const preferredBinary = binaries.data?.preferred ?? "";

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Settings</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label>Name</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label>Branch</Label>
            <select
              className="rounded-md border border-input bg-background px-3 py-2 text-sm"
              value={branchId}
              onChange={(e) => setBranchId(e.target.value)}
            >
              {branches.data?.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label>Server binary</Label>
            <select
              className="rounded-md border border-input bg-background px-3 py-2 text-sm"
              value={serverBinary}
              onChange={(e) => setServerBinary(e.target.value)}
            >
              <option value="">
                Auto{preferredBinary ? ` (${preferredBinary})` : " (prefer 64-bit)"}
              </option>
              {binaryOptions.map((b) => (
                <option key={b} value={b}>
                  {b}
                </option>
              ))}
              {serverBinary && !binaryOptions.includes(serverBinary) && (
                <option value={serverBinary}>{serverBinary} (missing)</option>
              )}
            </select>
            <p className="text-xs text-muted-foreground">
              Executables found in the branch install. Auto prefers arma3server_x64.
            </p>
          </div>
          <div className="flex flex-col gap-1">
            <Label>Modset</Label>
            <select
              className="rounded-md border border-input bg-background px-3 py-2 text-sm"
              value={modsetId}
              onChange={(e) => setModsetId(e.target.value)}
            >
              <option value="none">None</option>
              {modsets.data?.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground">
              Changing the modset reloads workshop mods from that modset on save (local mods are kept).
            </p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1">
              <Label>Game port</Label>
              <Input value={gamePort} onChange={(e) => setGamePort(e.target.value)} />
            </div>
            <div className="flex flex-col gap-1">
              <Label>Steam port</Label>
              <Input value={steamPort} onChange={(e) => setSteamPort(e.target.value)} />
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <Label>Profile name</Label>
            <Input value={profileName} onChange={(e) => setProfileName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label>Extra launch params</Label>
            <Input value={extra} onChange={(e) => setExtra(e.target.value)} placeholder="-nosound" />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
              Save
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => provision.mutate()}
              disabled={provision.isPending}
            >
              <RefreshCw className="size-3.5" /> Re-provision
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Launch command</CardTitle>
          <CardDescription>Preview of the command used when starting the server.</CardDescription>
        </CardHeader>
        <CardContent>
          <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">
            {launch.data?.command ?? "Loading..."}
          </pre>
          {launch.data && (
            <Button
              size="sm"
              variant="outline"
              className="mt-2"
              onClick={() => navigator.clipboard.writeText(launch.data!.command)}
            >
              <Copy className="size-3.5" /> Copy
            </Button>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function ModsTab({ serverId }: { serverId: number }) {
  const queryClient = useQueryClient();
  const serverMods = useQuery({
    queryKey: ["serverMods", serverId],
    queryFn: () => api.getServerMods(serverId),
  });
  const allMods = useQuery({ queryKey: ["mods"], queryFn: () => api.getMods() });

  const [mods, setMods] = useState<ServerMod[]>([]);
  const [customPath, setCustomPath] = useState("");
  const [folderPath, setFolderPath] = useState("");
  const [scanMsg, setScanMsg] = useState<string | null>(null);

  useEffect(() => {
    if (serverMods.data) setMods(serverMods.data);
  }, [serverMods.data]);

  const save = useMutation({
    mutationFn: () => api.putServerMods(serverId, mods),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["serverMods", serverId] });
      queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] });
      queryClient.invalidateQueries({ queryKey: ["launch", serverId] });
    },
  });

  const scanFolder = useMutation({
    mutationFn: () => api.discoverLocalMods(folderPath.trim()),
    onSuccess: (data) => {
      const found = data.mods ?? [];
      setMods((prev) => {
        const existing = new Set(
          prev
            .filter((m) => m.customPath)
            .map((m) => m.customPath!.replace(/[/\\]+$/, "").toLowerCase()),
        );
        const added: ServerMod[] = [];
        for (const f of found) {
          const key = f.path.replace(/[/\\]+$/, "").toLowerCase();
          if (existing.has(key)) continue;
          existing.add(key);
          added.push({
            customPath: f.path,
            label: f.name,
            isServerMod: false,
            enabled: true,
            source: "custom",
          });
        }
        setScanMsg(
          added.length
            ? `Added ${added.length} mod(s). Save & provision to apply.`
            : found.length
              ? "All discovered mods are already in the list."
              : "No mod folders found in that path.",
        );
        return added.length ? [...prev, ...added] : prev;
      });
    },
    onError: (err: Error) => {
      setScanMsg(err.message || "Failed to scan folder");
    },
  });

  const addManaged = (modId: number) => {
    const mod = allMods.data?.find((m) => m.id === modId);
    if (!mod) return;
    setMods((prev) => [
      ...prev,
      { modId, label: mod.name, isServerMod: false, enabled: true, source: "" },
    ]);
  };

  const addCustom = () => {
    if (!customPath.trim()) return;
    setMods((prev) => [
      ...prev,
      {
        customPath: customPath.trim(),
        label: customPath.split(/[/\\]/).pop() ?? "custom",
        isServerMod: false,
        enabled: true,
        source: "custom",
      },
    ]);
    setCustomPath("");
  };

  const sourceLabel = (m: ServerMod) => {
    if (m.modId) return `Workshop #${m.modId}`;
    return m.customPath ?? "Custom";
  };

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Add from folder</CardTitle>
          <CardDescription>
            Scan a directory for Arma mod subfolders and add them to the list. The folder path
            is not saved — only the individual mods you keep.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="flex flex-1 flex-col gap-1">
              <Label>Folder path</Label>
              <Input
                value={folderPath}
                onChange={(e) => {
                  setFolderPath(e.target.value);
                  setScanMsg(null);
                }}
                placeholder="/path/to/mods-root"
              />
            </div>
            <Button
              onClick={() => scanFolder.mutate()}
              disabled={scanFolder.isPending || !folderPath.trim()}
            >
              {scanFolder.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <RefreshCw className="size-3.5" />
              )}
              Scan & add
            </Button>
          </div>
          {scanMsg && <p className="text-xs text-muted-foreground">{scanMsg}</p>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Add mods</CardTitle>
          <CardDescription>
            Workshop downloads or a single custom mod path. Custom paths are mirrored with
            lowercase names for Linux.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex flex-1 flex-col gap-1">
            <Label>Managed mod</Label>
            <select
              className="rounded-md border border-input bg-background px-3 py-2 text-sm"
              defaultValue=""
              onChange={(e) => {
                if (e.target.value) addManaged(parseInt(e.target.value, 10));
                e.target.value = "";
              }}
            >
              <option value="">Select...</option>
              {allMods.data
                ?.filter((m) => m.downloaded)
                .map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
            </select>
          </div>
          <div className="flex flex-[2] flex-col gap-1">
            <Label>Custom mod path</Label>
            <Input
              value={customPath}
              onChange={(e) => setCustomPath(e.target.value)}
              placeholder="/path/to/mod"
            />
          </div>
          <Button variant="outline" onClick={addCustom}>
            Add custom
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">Loaded mods</CardTitle>
          <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
            Save & provision
          </Button>
        </CardHeader>
        <CardContent>
          {!mods.length ? (
            <p className="text-sm text-muted-foreground">No mods configured.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Enabled</TableHead>
                  <TableHead>Label</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead>Server mod</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {mods.map((m, i) => (
                  <TableRow
                    key={i}
                    className={m.enabled === false ? "opacity-50" : undefined}
                  >
                    <TableCell>
                      <input
                        type="checkbox"
                        checked={m.enabled !== false}
                        onChange={(e) =>
                          setMods((prev) =>
                            prev.map((x, j) =>
                              j === i ? { ...x, enabled: e.target.checked } : x,
                            ),
                          )
                        }
                      />
                    </TableCell>
                    <TableCell>{m.label}</TableCell>
                    <TableCell className="max-w-[240px] truncate text-xs text-muted-foreground">
                      {sourceLabel(m)}
                    </TableCell>
                    <TableCell>
                      <input
                        type="checkbox"
                        checked={m.isServerMod}
                        onChange={(e) =>
                          setMods((prev) =>
                            prev.map((x, j) =>
                              j === i ? { ...x, isServerMod: e.target.checked } : x,
                            ),
                          )
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setMods((prev) => prev.filter((_, j) => j !== i))}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function KeysTab({ serverId }: { serverId: number }) {
  const queryClient = useQueryClient();
  const keys = useQuery({
    queryKey: ["serverKeys", serverId],
    queryFn: () => api.getServerKeys(serverId),
  });
  const fileRef = useRef<HTMLInputElement>(null);

  const baseKeys = keys.data?.base ?? [];
  const modKeys = keys.data?.auto ?? [];
  const customKeys = keys.data?.custom ?? [];

  const rescan = useMutation({
    mutationFn: () => api.rescanServerKeys(serverId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] }),
  });

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadServerKey(serverId, file),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] }),
  });

  const remove = useMutation({
    mutationFn: (keyId: number) => api.deleteServerKey(serverId, keyId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] }),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">
          Keys are rebuilt on server start, when mods are saved, after mod downloads for
          assigned mods, and when you rescan here.
        </p>
        <Button
          size="sm"
          variant="outline"
          onClick={() => rescan.mutate()}
          disabled={rescan.isPending}
        >
          {rescan.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <RefreshCw className="size-3.5" />
          )}
          Rescan keys
        </Button>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Base game keys</CardTitle>
            <CardDescription>From the branch install (includes a3.bikey).</CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="text-sm">
              {baseKeys.map((k) => (
                <li key={k} className="text-muted-foreground">
                  {k}
                </li>
              ))}
              {!baseKeys.length && (
                <li className="text-muted-foreground">
                  None yet — install the branch and rescan, or start the server once.
                </li>
              )}
            </ul>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Mod keys</CardTitle>
            <CardDescription>Auto-extracted from configured mods (read-only).</CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="text-sm">
              {modKeys.map((k) => (
                <li key={k} className="text-muted-foreground">
                  {k}
                </li>
              ))}
              {!modKeys.length && (
                <li className="text-muted-foreground">
                  None yet — assign downloaded mods and rescan.
                </li>
              )}
            </ul>
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">Custom keys</CardTitle>
            <CardDescription>Always loaded in addition to base and mod keys.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <input
              ref={fileRef}
              type="file"
              accept=".bikey"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) upload.mutate(f);
                e.target.value = "";
              }}
            />
            <Button size="sm" variant="outline" onClick={() => fileRef.current?.click()}>
              <Upload className="size-3.5" /> Upload .bikey
            </Button>
            <ul className="text-sm">
              {customKeys.map((k) => (
                <li key={k.id} className="flex items-center justify-between gap-2">
                  <span>{k.filename}</span>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => remove.mutate(k.id)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function ConfigTab({ serverId, serverStatus }: { serverId: number; serverStatus: string }) {
  type ConfigKind = "server.cfg" | "basic.cfg" | "profile";

  const server = useQuery({
    queryKey: ["servers", serverId],
    queryFn: () => api.getServer(serverId),
  });
  const [kind, setKind] = useState<ConfigKind>("server.cfg");
  const [profileFile, setProfileFile] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [dirty, setDirty] = useState(false);

  const profileFiles = useQuery({
    queryKey: ["profileFiles", serverId, serverStatus],
    queryFn: () => api.listProfileFiles(serverId),
    enabled: kind === "profile",
  });

  const cfg = useQuery({
    queryKey: ["config", serverId, kind, profileFile],
    queryFn: () => {
      if (kind === "profile" && profileFile) {
        return api.getProfileFile(serverId, profileFile);
      }
      return api.getServerConfig(serverId, kind);
    },
    enabled: kind !== "profile" || !!profileFile,
  });

  useEffect(() => {
    if (kind !== "profile") return;
    if (profileFiles.data?.length && !profileFile) {
      setProfileFile(profileFiles.data[0].name);
    }
  }, [kind, profileFiles.data, profileFile]);

  useEffect(() => {
    if (cfg.data) {
      setContent(cfg.data.content);
      setDirty(false);
    }
  }, [cfg.data, kind, profileFile]);

  const save = useMutation({
    mutationFn: () => {
      if (kind === "profile" && profileFile) {
        return api.putProfileFile(serverId, profileFile, content);
      }
      return api.putServerConfig(serverId, kind, content);
    },
    onSuccess: () => setDirty(false),
  });

  const profileLabel = server.data?.profileName ?? "Server";
  const profileFolder = `profiles/home/${profileLabel}/`;

  return (
    <Card>
      <CardHeader className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap gap-2">
            {(["server.cfg", "basic.cfg", "profile"] as const).map((k) => (
              <Button
                key={k}
                size="sm"
                variant={kind === k ? "default" : "outline"}
                onClick={() => {
                  setKind(k);
                  setDirty(false);
                }}
              >
                {k === "profile" ? "Profile" : k}
              </Button>
            ))}
          </div>
          <Button size="sm" disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </div>
        {kind === "profile" && (
          <div className="flex flex-wrap items-center gap-3">
            <p className="text-xs text-muted-foreground">
              Profile folder: <code>{profileFolder}</code>
            </p>
            {profileFiles.data && profileFiles.data.length > 0 ? (
              <select
                className="rounded-md border border-input bg-background px-3 py-1.5 text-sm"
                value={profileFile ?? ""}
                onChange={(e) => {
                  setProfileFile(e.target.value);
                  setDirty(false);
                }}
              >
                {profileFiles.data.map((f) => (
                  <option key={f.name} value={f.name}>
                    {f.name}
                  </option>
                ))}
              </select>
            ) : (
              <p className="text-xs text-muted-foreground">
                No profile files yet — start the server once to generate them.
              </p>
            )}
          </div>
        )}
      </CardHeader>
      <CardContent>
        {cfg.isLoading ? (
          <Loader2 className="size-5 animate-spin" />
        ) : kind === "profile" && !profileFile ? (
          <p className="text-sm text-muted-foreground">
            No profile file selected. Run the server to create profile files in{" "}
            <code>{profileFolder}</code>.
          </p>
        ) : (
          <textarea
            className="min-h-[400px] w-full rounded-md border border-input bg-background p-3 font-mono text-xs"
            value={content}
            onChange={(e) => {
              setContent(e.target.value);
              setDirty(true);
            }}
          />
        )}
      </CardContent>
    </Card>
  );
}

function LogsTab({ serverId, isRunning }: { serverId: number; isRunning: boolean }) {
  const { text, activePath } = useServerLogs(serverId, isRunning);
  const logRef = useRef<HTMLPreElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const logs = useQuery({
    queryKey: ["serverLogs", serverId],
    queryFn: () => api.listServerLogs(serverId),
    refetchInterval: isRunning ? 2000 : false,
  });
  const [selected, setSelected] = useState<string | null>(null);
  const [archived, setArchived] = useState("");

  const viewerText = isRunning ? text || "Waiting for log output..." : archived || "Select a log file";

  useEffect(() => {
    if (!autoScroll || !logRef.current) return;
    logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [viewerText, autoScroll]);

  const loadArchived = useCallback(async (name: string) => {
    setSelected(name);
    const data = await api.getServerLog(serverId, name, 0);
    setArchived(data.content);
  }, [serverId]);

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card className="lg:col-span-1">
        <CardHeader>
          <CardTitle className="text-base">Log files</CardTitle>
          <CardDescription>
            Session logs and Arma RPT files under this server instance.
          </CardDescription>
        </CardHeader>
        <CardContent className="max-h-96 overflow-y-auto">
          <ul className="text-sm">
            {(logs.data ?? []).map((l) => (
              <li key={l.name}>
                <button
                  type="button"
                  className="w-full rounded px-2 py-1 text-left hover:bg-accent"
                  onClick={() => loadArchived(l.name)}
                >
                  <div className="font-medium">{l.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {formatDate(l.modTime)} · {(l.size / 1024).toFixed(1)} KB
                  </div>
                </button>
              </li>
            ))}
            {!logs.data?.length && (
              <li className="text-muted-foreground">No logs yet.</li>
            )}
          </ul>
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle className="text-base">
              {isRunning ? "Live tail" : selected ? selected : "Log viewer"}
            </CardTitle>
            {activePath && isRunning && (
              <CardDescription className="truncate text-xs">
                {activePath === "console" ? "Live server output" : activePath}
              </CardDescription>
            )}
          </div>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.target.checked)}
            />
            Auto-scroll
          </label>
        </CardHeader>
        <CardContent>
          <pre
            ref={logRef}
            className="max-h-[500px] overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap"
          >
            {viewerText}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}

function MissionsTab({ serverId }: { serverId: number }) {
  const queryClient = useQueryClient();
  const missions = useQuery({
    queryKey: ["missions", serverId],
    queryFn: () => api.listMissions(serverId),
  });
  const fileRef = useRef<HTMLInputElement>(null);
  const [drag, setDrag] = useState(false);

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadMission(serverId, file),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["missions", serverId] }),
  });

  const remove = useMutation({
    mutationFn: (name: string) => api.deleteMission(serverId, name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["missions", serverId] }),
  });

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDrag(false);
    const file = e.dataTransfer.files[0];
    if (file?.name.toLowerCase().endsWith(".pbo")) upload.mutate(file);
  };

  return (
    <div className="flex flex-col gap-4">
      <div
        className={`flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-8 transition-colors ${
          drag ? "border-primary bg-primary/5" : "border-border"
        }`}
        onDragOver={(e) => {
          e.preventDefault();
          setDrag(true);
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={onDrop}
      >
        <Upload className="size-8 text-muted-foreground" />
        <p className="text-sm text-muted-foreground">Drop .pbo files here or</p>
        <input
          ref={fileRef}
          type="file"
          accept=".pbo"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) upload.mutate(f);
            e.target.value = "";
          }}
        />
        <Button size="sm" variant="outline" onClick={() => fileRef.current?.click()}>
          Choose file
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Missions</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Size</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {missions.data?.map((m) => (
                <TableRow key={m.name}>
                  <TableCell>{m.name}</TableCell>
                  <TableCell>{(m.size / 1024 / 1024).toFixed(2)} MB</TableCell>
                  <TableCell>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        if (confirm(`Delete ${m.name}?`)) remove.mutate(m.name);
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {!missions.data?.length && (
                <TableRow>
                  <TableCell colSpan={3} className="text-muted-foreground">
                    No missions uploaded.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
