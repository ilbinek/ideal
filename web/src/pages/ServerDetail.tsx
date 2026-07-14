import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Copy,
  Download,
  Loader2,
  Play,
  RefreshCw,
  RotateCcw,
  Search,
  Square,
  Trash2,
  Upload,
} from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { patchServerStatus, useServers } from "@/hooks/useServers";
import { useHeadlessLogs, useServerLogs } from "@/hooks/useServerLogs";
import { formatDate, formatBytes, formatCpuPercent, formatBytesPerSec, cpuGaugeFill, memoryGaugeFill, formatMemoryOfHost } from "@/lib/utils";
import { HalfCircleGauge } from "@/components/HalfCircleGauge";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useToast } from "@/components/ui/toast";
import type {
  HeadlessClientSnapshot,
  ProcessMetrics,
  ServerMod,
  ServerStatus,
} from "@/lib/types";
import {
  applyServerCfgForm,
  defaultServerCfgForm,
  parseServerCfgForm,
  type ServerCfgSimpleForm,
} from "@/lib/serverCfg";

type Tab = "overview" | "mods" | "keys" | "config" | "logs" | "headless" | "missions";

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

  const [actionError, setActionError] = useState("");

  const start = useMutation({
    mutationFn: () => api.startServer(serverId),
    onMutate: () => {
      setActionError("");
      patchServerStatus(queryClient, serverId, "starting");
    },
    onSuccess: () => {
      setActionError("");
      patchServerStatus(queryClient, serverId, "running");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Start failed");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
  });
  const stop = useMutation({
    mutationFn: () => api.stopServer(serverId),
    onMutate: () => {
      setActionError("");
      patchServerStatus(queryClient, serverId, "stopping");
    },
    onSuccess: () => {
      setActionError("");
      patchServerStatus(queryClient, serverId, "stopped");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Stop failed");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
  });
  const restart = useMutation({
    mutationFn: () => api.restartServer(serverId),
    onMutate: () => {
      setActionError("");
      patchServerStatus(queryClient, serverId, "stopping");
    },
    onSuccess: () => {
      setActionError("");
      patchServerStatus(queryClient, serverId, "running");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Restart failed");
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers"] });
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
    { id: "headless", label: "Headless Clients" },
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
            disabled={start.isPending || stop.isPending || restart.isPending}
            onClick={() => (isRunning ? stop.mutate() : start.mutate())}
          >
            {isRunning ? <Square className="size-3.5" /> : <Play className="size-3.5" />}
            {isRunning ? "Stop" : "Start"}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={restart.isPending || start.isPending || stop.isPending}
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

      {actionError && (
        <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {actionError}
        </p>
      )}

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
      {tab === "headless" && (
        <HeadlessTab serverId={serverId} serverRunning={isRunning} />
      )}
      {tab === "missions" && <MissionsTab serverId={serverId} />}
    </div>
  );
}

function useNetPeak(bps: number): number {
  const floor = 1024 * 1024; // 1 MiB/s so quiet links still show motion
  const peakRef = useRef(floor);
  if (Number.isFinite(bps) && bps > peakRef.current) {
    peakRef.current = bps;
  }
  return Math.max(peakRef.current, floor);
}

function ProcessMetricsStrip({ metrics }: { metrics?: ProcessMetrics }) {
  const recv = metrics?.netRecvBytesPerSec ?? 0;
  const sent = metrics?.netSentBytesPerSec ?? 0;
  const recvPeak = useNetPeak(recv);
  const sentPeak = useNetPeak(sent);
  if (!metrics) return null;

  const cpuFill = cpuGaugeFill(metrics.cpuPercent, metrics.cpuCount);
  const hostCpuFill = Math.min(1, Math.max(0, (metrics.hostCpuPercent ?? 0) / 100));
  const memFill = memoryGaugeFill(metrics.memoryBytes, metrics.memoryTotalBytes);
  const hostMemFill = memoryGaugeFill(metrics.memoryUsedBytes, metrics.memoryTotalBytes);
  const memPct = formatMemoryOfHost(metrics.memoryBytes, metrics.memoryTotalBytes);
  const cores = metrics.cpuCount > 0 ? metrics.cpuCount : 1;

  return (
    <div className="flex flex-wrap items-end gap-4 sm:gap-6">
      <HalfCircleGauge
        value={cpuFill}
        hostValue={hostCpuFill}
        label="CPU"
        display={formatCpuPercent(metrics.cpuPercent)}
        detail={`host ${formatCpuPercent(metrics.hostCpuPercent)} · ${cores}c`}
      />
      <HalfCircleGauge
        value={memFill}
        hostValue={hostMemFill}
        label="Memory"
        display={memPct}
        detail={`${formatBytes(metrics.memoryUsedBytes)} / ${formatBytes(metrics.memoryTotalBytes)} host`}
      />
      <HalfCircleGauge
        value={recv / recvPeak}
        label="Net ↓"
        display={formatBytesPerSec(recv)}
        detail="host RX"
      />
      <HalfCircleGauge
        value={sent / sentPeak}
        label="Net ↑"
        display={formatBytesPerSec(sent)}
        detail="host TX"
      />
      <div className="mb-1 flex flex-col gap-0.5 pb-1 text-sm">
        <span className="text-xs font-medium text-muted-foreground">Threads</span>
        <span className="text-lg font-semibold tabular-nums leading-none">{metrics.threads}</span>
      </div>
    </div>
  );
}

function LiveMetricsCard({ serverId }: { serverId: number }) {
  const { statusFor } = useServers();
  const live = statusFor(serverId);
  const status = live?.status;
  const show =
    (status === "running" || status === "starting" || status === "stopping") &&
    !!live?.metrics;
  if (!show || !live?.metrics) return null;

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-base">Process</CardTitle>
        <CardDescription>
          Live stats for PID {live.pid > 0 ? live.pid : "—"}
          {live.uptime > 0 ? ` · ${live.uptime}s uptime` : ""}
          {" · "}grey = host total · network is host-wide
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ProcessMetricsStrip metrics={live.metrics} />
      </CardContent>
    </Card>
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
    enabledCdlcs?: string[];
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
  const [enabledCdlcs, setEnabledCdlcs] = useState<string[]>(srv.enabledCdlcs ?? []);

  const branchIdNum = parseInt(branchId, 10);
  const binaries = useQuery({
    queryKey: ["branchBinaries", branchIdNum],
    queryFn: () => api.getBranchBinaries(branchIdNum),
    enabled: Number.isFinite(branchIdNum) && branchIdNum > 0,
  });
  const cdlcs = useQuery({
    queryKey: ["cdlcs"],
    queryFn: () => api.getCdlcs(),
  });

  useEffect(() => {
    setModsetId(srv.modsetId ? String(srv.modsetId) : "none");
  }, [srv.modsetId]);

  useEffect(() => {
    setServerBinary(srv.serverBinary ?? "");
  }, [srv.serverBinary]);

  useEffect(() => {
    setEnabledCdlcs(srv.enabledCdlcs ?? []);
  }, [srv.enabledCdlcs]);

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
        enabledCdlcs,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
      queryClient.invalidateQueries({ queryKey: ["serverMods", serverId] });
      queryClient.invalidateQueries({ queryKey: ["serverKeys", serverId] });
      queryClient.invalidateQueries({ queryKey: ["launch", serverId] });
      queryClient.invalidateQueries({ queryKey: ["headless", serverId] });
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
    <div className="flex flex-col gap-4">
      <LiveMetricsCard serverId={serverId} />

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
          <div className="flex flex-col gap-2">
            <Label>Creator DLCs</Label>
            {!cdlcs.data?.installed ? (
              <p className="text-xs text-muted-foreground">
                Install the{" "}
                <Link to="/branches" className="underline underline-offset-2">
                  Creator DLC
                </Link>{" "}
                branch to enable cDLC packs on this server.
              </p>
            ) : (
              <div className="flex flex-col gap-1.5">
                {cdlcs.data.items.map((item) => {
                  const checked = enabledCdlcs.includes(item.code);
                  return (
                    <label
                      key={item.code}
                      className={`flex items-center gap-2 text-sm ${
                        item.available ? "cursor-pointer" : "cursor-not-allowed opacity-50"
                      }`}
                    >
                      <input
                        type="checkbox"
                        className="size-4 rounded border-input"
                        checked={checked}
                        disabled={!item.available}
                        onChange={(e) => {
                          setEnabledCdlcs((prev) =>
                            e.target.checked
                              ? [...prev, item.code]
                              : prev.filter((c) => c !== item.code),
                          );
                        }}
                      />
                      <span>
                        {item.name}{" "}
                        <span className="text-muted-foreground">({item.code})</span>
                      </span>
                    </label>
                  );
                })}
                <p className="text-xs text-muted-foreground">
                  Enabled packs are symlinked from the Creator DLC branch and prepended to -mod=.
                </p>
              </div>
            )}
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
  type ServerCfgView = "simple" | "raw";

  const server = useQuery({
    queryKey: ["servers", serverId],
    queryFn: () => api.getServer(serverId),
  });
  const [kind, setKind] = useState<ConfigKind>("server.cfg");
  const [serverCfgView, setServerCfgView] = useState<ServerCfgView>("simple");
  const [profileFile, setProfileFile] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [form, setForm] = useState<ServerCfgSimpleForm>(defaultServerCfgForm);
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
      if (kind === "server.cfg") {
        setForm(parseServerCfgForm(cfg.data.content));
      }
      setDirty(false);
    }
  }, [cfg.data, kind, profileFile]);

  const updateForm = <K extends keyof ServerCfgSimpleForm>(
    key: K,
    value: ServerCfgSimpleForm[K],
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }));
    setDirty(true);
  };

  const switchServerCfgView = (next: ServerCfgView) => {
    if (next === serverCfgView) return;
    if (next === "raw") {
      setContent(applyServerCfgForm(content, form));
    } else {
      setForm(parseServerCfgForm(content));
    }
    setServerCfgView(next);
  };

  const save = useMutation({
    mutationFn: () => {
      if (kind === "profile" && profileFile) {
        return api.putProfileFile(serverId, profileFile, content);
      }
      let toSave = content;
      if (kind === "server.cfg" && serverCfgView === "simple") {
        toSave = applyServerCfgForm(content, form);
        setContent(toSave);
      }
      return api.putServerConfig(serverId, kind, toSave);
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
                  if (kind === "server.cfg" && serverCfgView === "simple" && dirty) {
                    setContent(applyServerCfgForm(content, form));
                  }
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
        {kind === "server.cfg" && (
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant={serverCfgView === "simple" ? "default" : "outline"}
              onClick={() => switchServerCfgView("simple")}
            >
              Simple
            </Button>
            <Button
              size="sm"
              variant={serverCfgView === "raw" ? "default" : "outline"}
              onClick={() => switchServerCfgView("raw")}
            >
              Raw
            </Button>
            <p className="text-xs text-muted-foreground">
              Simple edits the common settings; Raw opens the full server.cfg.
            </p>
          </div>
        )}
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
        ) : kind === "server.cfg" && serverCfgView === "simple" ? (
          <ServerCfgSimpleFormFields form={form} onChange={updateForm} />
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

function ServerCfgSimpleFormFields({
  form,
  onChange,
}: {
  form: ServerCfgSimpleForm;
  onChange: <K extends keyof ServerCfgSimpleForm>(
    key: K,
    value: ServerCfgSimpleForm[K],
  ) => void;
}) {
  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4">
      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-hostname">Server name</Label>
        <Input
          id="cfg-hostname"
          value={form.hostname}
          onChange={(e) => onChange("hostname", e.target.value)}
        />
        <p className="text-xs text-muted-foreground">Shown in the multiplayer server browser.</p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-password">Join password</Label>
        <Input
          id="cfg-password"
          type="password"
          autoComplete="new-password"
          value={form.password}
          onChange={(e) => onChange("password", e.target.value)}
          placeholder="Leave empty for a public server"
        />
        <p className="text-xs text-muted-foreground">Required for players to join. Empty = public.</p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-passwordAdmin">Admin password</Label>
        <Input
          id="cfg-passwordAdmin"
          type="password"
          autoComplete="new-password"
          value={form.passwordAdmin}
          onChange={(e) => onChange("passwordAdmin", e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          Used with <code>#login</code> in chat for admin commands.
        </p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-serverCommandPassword">Server command password</Label>
        <Input
          id="cfg-serverCommandPassword"
          type="password"
          autoComplete="new-password"
          value={form.serverCommandPassword}
          onChange={(e) => onChange("serverCommandPassword", e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          Optional password for server-side scripting commands.
        </p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-maxPlayers">Max players</Label>
        <Input
          id="cfg-maxPlayers"
          type="number"
          min={1}
          max={255}
          value={form.maxPlayers}
          onChange={(e) =>
            onChange("maxPlayers", Math.max(1, Number(e.target.value) || 1))
          }
        />
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-verifySignatures">Signature check</Label>
        <Select
          id="cfg-verifySignatures"
          value={String(form.verifySignatures)}
          onChange={(e) => onChange("verifySignatures", Number(e.target.value))}
        >
          <option value="0">Off — allow any mods</option>
          <option value="1">Weak — warn on mismatch</option>
          <option value="2">Full — kick unsigned / mismatched mods</option>
        </Select>
        <p className="text-xs text-muted-foreground">
          Full is recommended for public servers.
        </p>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="cfg-allowedFilePatching">File patching</Label>
        <Select
          id="cfg-allowedFilePatching"
          value={String(form.allowedFilePatching)}
          onChange={(e) => onChange("allowedFilePatching", Number(e.target.value))}
        >
          <option value="0">No one</option>
          <option value="1">Headless clients only</option>
          <option value="2">Anyone</option>
        </Select>
        <p className="text-xs text-muted-foreground">
          Usually leave as &quot;No one&quot; unless you need -filePatching.
        </p>
      </div>

      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={form.BattlEye}
          onChange={(e) => onChange("BattlEye", e.target.checked)}
        />
        <span>
          <span className="font-medium">BattlEye</span>
          <span className="block text-xs text-muted-foreground">
            Anti-cheat. Keep enabled for public servers.
          </span>
        </span>
      </label>

      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={form.persistent}
          onChange={(e) => onChange("persistent", e.target.checked)}
        />
        <span>
          <span className="font-medium">Persistent</span>
          <span className="block text-xs text-muted-foreground">
            Keep the mission running when the last player leaves.
          </span>
        </span>
      </label>

      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={form.drawingOnMap}
          onChange={(e) => onChange("drawingOnMap", e.target.checked)}
        />
        <span>
          <span className="font-medium">Map drawing</span>
          <span className="block text-xs text-muted-foreground">
            Allow players to draw on the in-game map.
          </span>
        </span>
      </label>
    </div>
  );
}

function LogsTab({ serverId, isRunning }: { serverId: number; isRunning: boolean }) {
  const queryClient = useQueryClient();
  const { text, activePath } = useServerLogs(serverId, isRunning);
  const logRef = useRef<HTMLPreElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const [view, setView] = useState<"live" | "file">(isRunning ? "live" : "file");
  const [logError, setLogError] = useState("");
  const logs = useQuery({
    queryKey: ["serverLogs", serverId],
    queryFn: () => api.listServerLogs(serverId),
    refetchInterval: isRunning ? 2000 : false,
  });
  const [selected, setSelected] = useState<string | null>(null);
  const [archived, setArchived] = useState("");

  useEffect(() => {
    if (isRunning) {
      setView((v) => (v === "file" && selected ? "file" : "live"));
    } else {
      setView("file");
    }
  }, [isRunning, selected]);

  const showingLive = isRunning && view === "live";
  const viewerText = showingLive
    ? text || "Waiting for log output..."
    : archived || "Select a log file";

  useEffect(() => {
    if (!autoScroll || !logRef.current) return;
    logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [viewerText, autoScroll]);

  const loadArchived = useCallback(
    async (name: string) => {
      setLogError("");
      setSelected(name);
      setView("file");
      const data = await api.getServerLog(serverId, name, 0);
      setArchived(data.content);
    },
    [serverId],
  );

  const remove = useMutation({
    mutationFn: (name: string) => api.deleteServerLog(serverId, name),
    onSuccess: (_data, name) => {
      setLogError("");
      if (selected === name) {
        setSelected(null);
        setArchived("");
        setView(isRunning ? "live" : "file");
      }
      queryClient.invalidateQueries({ queryKey: ["serverLogs", serverId] });
    },
    onError: (err) => {
      setLogError(err instanceof Error ? err.message : "Failed to delete log");
    },
  });

  const download = useMutation({
    mutationFn: async (name: string | null) => {
      if (name) {
        await api.downloadServerLog(serverId, name);
        return;
      }
      // Live buffer download
      const blob = new Blob([text || ""], { type: "text/plain;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `live-${serverId}-${new Date().toISOString().replace(/[:.]/g, "-")}.log`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    },
    onError: (err) => {
      setLogError(err instanceof Error ? err.message : "Failed to download log");
    },
  });

  const activeSessionName = activePath ? activePath.split(/[/\\]/).pop() : "";

  return (
    <div className="flex flex-col gap-4">
      <LiveMetricsCard serverId={serverId} />

      <div className="grid gap-4 lg:grid-cols-3">
      <Card className="lg:col-span-1">
        <CardHeader>
          <CardTitle className="text-base">Log files</CardTitle>
          <CardDescription>
            Session logs and Arma RPT files under this server instance.
          </CardDescription>
        </CardHeader>
        <CardContent className="max-h-96 overflow-y-auto">
          {logError && (
            <p className="mb-2 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
              {logError}
            </p>
          )}
          <ul className="text-sm">
            {isRunning && (
              <li>
                <button
                  type="button"
                  className={`mb-1 w-full rounded px-2 py-1 text-left hover:bg-accent ${
                    showingLive ? "bg-accent" : ""
                  }`}
                  onClick={() => setView("live")}
                >
                  <div className="font-medium">Live tail</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {activePath
                      ? activePath === "console"
                        ? "Live server output"
                        : activePath
                      : "Streaming…"}
                  </div>
                </button>
              </li>
            )}
            {(logs.data ?? []).map((l) => {
              const isActiveLive =
                isRunning &&
                (l.name === activeSessionName ||
                  (activePath && activePath.endsWith(l.name)));
              return (
                <li
                  key={l.name}
                  className={`group flex items-start gap-1 rounded ${
                    !showingLive && selected === l.name ? "bg-accent" : ""
                  }`}
                >
                  <button
                    type="button"
                    className="min-w-0 flex-1 px-2 py-1 text-left hover:bg-accent/80"
                    onClick={() => loadArchived(l.name)}
                  >
                    <div className="truncate font-medium">{l.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {formatDate(l.modTime)} · {(l.size / 1024).toFixed(1)} KB
                      {isActiveLive ? " · active" : ""}
                    </div>
                  </button>
                  <div className="flex shrink-0 gap-0.5 py-1 pr-1 opacity-70 group-hover:opacity-100">
                    <Button
                      size="sm"
                      variant="ghost"
                      className="size-7 p-0"
                      title="Download"
                      disabled={download.isPending}
                      onClick={(e) => {
                        e.stopPropagation();
                        setLogError("");
                        download.mutate(l.name);
                      }}
                    >
                      <Download className="size-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="size-7 p-0"
                      title={isActiveLive ? "Stop the server to delete the active log" : "Delete"}
                      disabled={remove.isPending || !!isActiveLive}
                      onClick={(e) => {
                        e.stopPropagation();
                        if (!confirm(`Delete ${l.name}?`)) return;
                        remove.mutate(l.name);
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </li>
              );
            })}
            {!logs.data?.length && (
              <li className="text-muted-foreground">No logs yet.</li>
            )}
          </ul>
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="text-base">
              {showingLive ? "Live tail" : selected ? selected : "Log viewer"}
            </CardTitle>
            {showingLive && activePath && (
              <CardDescription className="truncate text-xs">
                {activePath === "console" ? "Live server output" : activePath}
              </CardDescription>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
              />
              Auto-scroll
            </label>
            <Button
              size="sm"
              variant="outline"
              disabled={download.isPending || (!showingLive && !selected)}
              onClick={() => {
                setLogError("");
                download.mutate(showingLive ? null : selected);
              }}
            >
              <Download className="size-3.5" />
              {showingLive ? "Save live buffer" : "Download"}
            </Button>
            {!showingLive && selected && (
              <Button
                size="sm"
                variant="outline"
                disabled={remove.isPending}
                onClick={() => {
                  if (!confirm(`Delete ${selected}?`)) return;
                  remove.mutate(selected);
                }}
              >
                <Trash2 className="size-3.5" />
                Delete
              </Button>
            )}
          </div>
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
    </div>
  );
}

function isPboFile(file: File): boolean {
  return file.name.toLowerCase().endsWith(".pbo");
}

function MissionsTab({ serverId }: { serverId: number }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const missions = useQuery({
    queryKey: ["missions", serverId],
    queryFn: () => api.listMissions(serverId),
  });
  const fileRef = useRef<HTMLInputElement>(null);
  const [drag, setDrag] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [filter, setFilter] = useState("");

  const missionNames = useMemo(
    () => new Set((missions.data ?? []).map((m) => m.name)),
    [missions.data],
  );

  const filteredMissions = useMemo(() => {
    const list = missions.data ?? [];
    const q = filter.trim().toLowerCase();
    if (!q) return list;
    return list.filter((m) => m.name.toLowerCase().includes(q));
  }, [missions.data, filter]);

  useEffect(() => {
    setSelected((prev) => {
      let changed = false;
      const next = new Set<string>();
      for (const name of prev) {
        if (missionNames.has(name)) next.add(name);
        else changed = true;
      }
      return changed || next.size !== prev.size ? next : prev;
    });
  }, [missionNames]);

  const visibleNames = filteredMissions.map((m) => m.name);
  const allSelected =
    visibleNames.length > 0 && visibleNames.every((n) => selected.has(n));
  const someSelected =
    visibleNames.some((n) => selected.has(n)) && !allSelected;

  const toggleOne = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const toggleAll = () => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allSelected) {
        for (const name of visibleNames) next.delete(name);
      } else {
        for (const name of visibleNames) next.add(name);
      }
      return next;
    });
  };

  const upload = useMutation({
    mutationFn: async (files: File[]) => {
      let ok = 0;
      const failed: string[] = [];
      for (const file of files) {
        try {
          await api.uploadMission(serverId, file);
          ok++;
        } catch (e) {
          failed.push(
            `${file.name}${e instanceof ApiError ? `: ${e.message}` : ""}`,
          );
        }
      }
      return { ok, failed };
    },
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ["missions", serverId] });
      if (res.failed.length === 0) {
        toast.success(
          res.ok === 1 ? "Mission uploaded" : `Uploaded ${res.ok} missions`,
        );
      } else if (res.ok === 0) {
        toast.error("Upload failed", res.failed.join("; "));
      } else {
        toast.error(
          `Uploaded ${res.ok}, failed ${res.failed.length}`,
          res.failed.join("; "),
        );
      }
    },
    onError: (e) =>
      toast.error("Upload failed", e instanceof ApiError ? e.message : undefined),
  });

  const remove = useMutation({
    mutationFn: (name: string) => api.deleteMission(serverId, name),
    onSuccess: (_data, name) => {
      setSelected((prev) => {
        if (!prev.has(name)) return prev;
        const next = new Set(prev);
        next.delete(name);
        return next;
      });
      queryClient.invalidateQueries({ queryKey: ["missions", serverId] });
      toast.success("Mission deleted");
    },
    onError: (e) =>
      toast.error("Delete failed", e instanceof ApiError ? e.message : undefined),
  });

  const removeMany = useMutation({
    mutationFn: (names: string[]) => api.deleteMissions(serverId, names),
    onSuccess: (res) => {
      setSelected(new Set());
      queryClient.invalidateQueries({ queryKey: ["missions", serverId] });
      if (res.failed.length === 0) {
        toast.success(
          res.deleted.length === 1
            ? "Mission deleted"
            : `Deleted ${res.deleted.length} missions`,
        );
      } else if (res.deleted.length === 0) {
        toast.error(
          "Delete failed",
          res.failed.map((f) => `${f.name}: ${f.error}`).join("; "),
        );
      } else {
        toast.error(
          `Deleted ${res.deleted.length}, failed ${res.failed.length}`,
          res.failed.map((f) => `${f.name}: ${f.error}`).join("; "),
        );
      }
    },
    onError: (e) =>
      toast.error("Delete failed", e instanceof ApiError ? e.message : undefined),
  });

  const downloadOne = useMutation({
    mutationFn: (name: string) => api.downloadMission(serverId, name),
    onError: (e) =>
      toast.error("Download failed", e instanceof ApiError ? e.message : undefined),
  });

  const downloadMany = useMutation({
    mutationFn: async (names: string[]) => {
      const failed: string[] = [];
      for (const name of names) {
        try {
          await api.downloadMission(serverId, name);
        } catch (e) {
          failed.push(`${name}${e instanceof ApiError ? `: ${e.message}` : ""}`);
        }
      }
      return { ok: names.length - failed.length, failed };
    },
    onSuccess: (res) => {
      if (res.failed.length === 0) {
        toast.success(
          res.ok === 1 ? "Download started" : `Downloading ${res.ok} missions`,
        );
      } else if (res.ok === 0) {
        toast.error("Download failed", res.failed.join("; "));
      } else {
        toast.error(
          `Downloaded ${res.ok}, failed ${res.failed.length}`,
          res.failed.join("; "),
        );
      }
    },
    onError: (e) =>
      toast.error("Download failed", e instanceof ApiError ? e.message : undefined),
  });

  const uploadPbos = (list: FileList | File[]) => {
    const files = Array.from(list).filter(isPboFile);
    if (files.length === 0) {
      toast.error("No .pbo files selected");
      return;
    }
    upload.mutate(files);
  };

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDrag(false);
    uploadPbos(e.dataTransfer.files);
  };

  const busy =
    upload.isPending ||
    remove.isPending ||
    removeMany.isPending ||
    downloadOne.isPending ||
    downloadMany.isPending;

  const selectedList = Array.from(selected);

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
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files?.length) uploadPbos(e.target.files);
            e.target.value = "";
          }}
        />
        <Button
          size="sm"
          variant="outline"
          disabled={upload.isPending}
          onClick={() => fileRef.current?.click()}
        >
          {upload.isPending ? (
            <>
              <Loader2 className="size-4 animate-spin" />
              Uploading…
            </>
          ) : (
            "Choose files"
          )}
        </Button>
      </div>

      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
          <CardTitle className="text-base">Missions</CardTitle>
          {selected.size > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm text-muted-foreground">
                {selected.size} selected
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onClick={() => downloadMany.mutate(selectedList)}
              >
                {downloadMany.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Download className="size-3.5" />
                )}
                Download
              </Button>
              <Button
                size="sm"
                variant="destructive"
                disabled={busy}
                onClick={() => {
                  if (
                    !confirm(
                      `Delete ${selected.size} mission${selected.size === 1 ? "" : "s"}?`,
                    )
                  ) {
                    return;
                  }
                  removeMany.mutate(selectedList);
                }}
              >
                {removeMany.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Trash2 className="size-3.5" />
                )}
                Delete
              </Button>
            </div>
          )}
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter missions…"
              className="pl-8"
              aria-label="Filter missions"
            />
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10">
                  <input
                    type="checkbox"
                    className="size-4 rounded border-input"
                    checked={allSelected}
                    ref={(el) => {
                      if (el) el.indeterminate = someSelected;
                    }}
                    disabled={!visibleNames.length || busy}
                    onChange={toggleAll}
                    aria-label="Select all visible missions"
                  />
                </TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Size</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredMissions.map((m) => {
                const checked = selected.has(m.name);
                return (
                  <TableRow key={m.name} data-state={checked ? "selected" : undefined}>
                    <TableCell>
                      <input
                        type="checkbox"
                        className="size-4 rounded border-input"
                        checked={checked}
                        disabled={busy}
                        onChange={() => toggleOne(m.name)}
                        aria-label={`Select ${m.name}`}
                      />
                    </TableCell>
                    <TableCell className="font-medium">{m.name}</TableCell>
                    <TableCell>{(m.size / 1024 / 1024).toFixed(2)} MB</TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={busy}
                          onClick={() => downloadOne.mutate(m.name)}
                          aria-label={`Download ${m.name}`}
                        >
                          <Download className="size-3.5" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={busy}
                          onClick={() => {
                            if (confirm(`Delete ${m.name}?`)) remove.mutate(m.name);
                          }}
                          aria-label={`Delete ${m.name}`}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
              {!missions.data?.length && (
                <TableRow>
                  <TableCell colSpan={4} className="text-muted-foreground">
                    No missions uploaded.
                  </TableCell>
                </TableRow>
              )}
              {!!missions.data?.length && !filteredMissions.length && (
                <TableRow>
                  <TableCell colSpan={4} className="text-muted-foreground">
                    No missions match “{filter.trim()}”.
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

function statusBadge(status: ServerStatus) {
  return <Badge variant="secondary">{status}</Badge>;
}

function HeadlessTab({
  serverId,
  serverRunning,
}: {
  serverId: number;
  serverRunning: boolean;
}) {
  const queryClient = useQueryClient();
  const { headlessFor } = useServers();
  const liveClients = headlessFor(serverId);

  const headless = useQuery({
    queryKey: ["headless", serverId],
    queryFn: () => api.getHeadless(serverId),
    refetchInterval: (query) => {
      const clients = query.state.data?.clients ?? [];
      return clients.some((c) => c.status === "starting" || c.status === "stopping")
        ? 2000
        : false;
    },
  });

  const [countInput, setCountInput] = useState("0");
  const [selectedSlot, setSelectedSlot] = useState<number | null>(null);
  const [actionError, setActionError] = useState("");

  useEffect(() => {
    if (headless.data) {
      setCountInput(String(headless.data.count));
    }
  }, [headless.data?.count]);

  // Prefer live SSE status, fall back to REST payload.
  const bySlot = new Map<number, HeadlessClientSnapshot>();
  for (const c of headless.data?.clients ?? []) {
    bySlot.set(c.slot, c);
  }
  for (const c of liveClients) {
    bySlot.set(c.slot, c);
  }
  const mergedClients = Array.from(bySlot.values()).sort((a, b) => a.slot - b.slot);
  const slotKey = mergedClients.map((c) => c.slot).join(",");

  useEffect(() => {
    if (!slotKey) {
      setSelectedSlot(null);
      return;
    }
    const slots = slotKey.split(",").map((s) => parseInt(s, 10));
    setSelectedSlot((prev) =>
      prev != null && slots.includes(prev) ? prev : slots[0],
    );
  }, [slotKey]);

  const selected = mergedClients.find((c) => c.slot === selectedSlot) ?? null;
  const hcRunning =
    selected?.status === "running" || selected?.status === "starting";

  const setCount = useMutation({
    mutationFn: () => api.setHeadlessCount(serverId, parseInt(countInput, 10) || 0),
    onSuccess: () => {
      setActionError("");
      queryClient.invalidateQueries({ queryKey: ["headless", serverId] });
      queryClient.invalidateQueries({ queryKey: ["servers", serverId] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Failed to update count");
    },
  });

  const startHc = useMutation({
    mutationFn: (slot: number) => api.startHeadless(serverId, slot),
    onSuccess: () => {
      setActionError("");
      queryClient.invalidateQueries({ queryKey: ["headless", serverId] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Start failed");
    },
  });

  const stopHc = useMutation({
    mutationFn: (slot: number) => api.stopHeadless(serverId, slot),
    onSuccess: () => {
      setActionError("");
      queryClient.invalidateQueries({ queryKey: ["headless", serverId] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Stop failed");
    },
  });

  const restartHc = useMutation({
    mutationFn: (slot: number) => api.restartHeadless(serverId, slot),
    onSuccess: () => {
      setActionError("");
      queryClient.invalidateQueries({ queryKey: ["headless", serverId] });
    },
    onError: (err) => {
      setActionError(err instanceof Error ? err.message : "Restart failed");
    },
  });

  const busy =
    setCount.isPending ||
    startHc.isPending ||
    stopHc.isPending ||
    restartHc.isPending;

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Headless clients</CardTitle>
          <CardDescription>
            Run local headless clients against this server (
            <code className="text-xs">-client -connect=127.0.0.1</code>
            ). They start automatically when the dedicated server starts.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="hc-count">Count</Label>
              <Input
                id="hc-count"
                type="number"
                min={0}
                max={16}
                className="w-28"
                value={countInput}
                onChange={(e) => setCountInput(e.target.value)}
              />
            </div>
            <Button
              size="sm"
              disabled={busy || !Number.isFinite(parseInt(countInput, 10))}
              onClick={() => setCount.mutate()}
            >
              {setCount.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : null}
              Apply
            </Button>
            {!serverRunning && (parseInt(countInput, 10) || 0) > 0 && (
              <p className="text-xs text-muted-foreground">
                Start the dedicated server to launch headless clients.
              </p>
            )}
          </div>

          {actionError && (
            <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {actionError}
            </p>
          )}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Profile</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>PID</TableHead>
                <TableHead>CPU</TableHead>
                <TableHead>Memory</TableHead>
                <TableHead>Threads</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {mergedClients.map((c) => {
                const running = c.status === "running" || c.status === "starting";
                return (
                  <TableRow
                    key={c.slot}
                    className={selectedSlot === c.slot ? "bg-accent/40" : undefined}
                    onClick={() => setSelectedSlot(c.slot)}
                  >
                    <TableCell className="font-medium">{c.profileName}</TableCell>
                    <TableCell>{statusBadge(c.status)}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {c.pid > 0 ? c.pid : "—"}
                    </TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">
                      {running ? formatCpuPercent(c.metrics?.cpuPercent) : "—"}
                    </TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">
                      {running && c.metrics
                        ? `${formatBytes(c.metrics.memoryBytes)} (${formatMemoryOfHost(c.metrics.memoryBytes, c.metrics.memoryTotalBytes)})`
                        : "—"}
                    </TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">
                      {running && c.metrics ? c.metrics.threads : "—"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div
                        className="flex justify-end gap-1"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy || (!serverRunning && !running)}
                          onClick={() =>
                            running ? stopHc.mutate(c.slot) : startHc.mutate(c.slot)
                          }
                        >
                          {running ? (
                            <Square className="size-3.5" />
                          ) : (
                            <Play className="size-3.5" />
                          )}
                          {running ? "Stop" : "Start"}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy || !serverRunning}
                          onClick={() => restartHc.mutate(c.slot)}
                        >
                          <RotateCcw className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
              {!mergedClients.length && (
                <TableRow>
                  <TableCell colSpan={7} className="text-muted-foreground">
                    No headless clients configured. Set a count above to create slots.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {selected && (
        <HeadlessLogsPanel
          serverId={serverId}
          slot={selected.slot}
          profileName={selected.profileName}
          isRunning={!!hcRunning}
        />
      )}
    </div>
  );
}

function HeadlessLogsPanel({
  serverId,
  slot,
  profileName,
  isRunning,
}: {
  serverId: number;
  slot: number;
  profileName: string;
  isRunning: boolean;
}) {
  const queryClient = useQueryClient();
  const { text, activePath } = useHeadlessLogs(serverId, slot, isRunning);
  const logRef = useRef<HTMLPreElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const [view, setView] = useState<"live" | "file">(isRunning ? "live" : "file");
  const [selected, setSelected] = useState<string | null>(null);
  const [archived, setArchived] = useState("");
  const [logError, setLogError] = useState("");

  const logs = useQuery({
    queryKey: ["headlessLogs", serverId, slot],
    queryFn: () => api.listHeadlessLogs(serverId, slot),
    refetchInterval: isRunning ? 2000 : false,
  });

  useEffect(() => {
    setSelected(null);
    setArchived("");
    setLogError("");
    setView(isRunning ? "live" : "file");
  }, [slot, isRunning]);

  const showingLive = isRunning && view === "live";
  const viewerText = showingLive
    ? text || "Waiting for log output..."
    : archived || "Select a log file";

  useEffect(() => {
    if (!autoScroll || !logRef.current) return;
    logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [viewerText, autoScroll]);

  const loadArchived = useCallback(
    async (name: string) => {
      setLogError("");
      setSelected(name);
      setView("file");
      const data = await api.getHeadlessLog(serverId, slot, name, 0);
      setArchived(data.content);
    },
    [serverId, slot],
  );

  const remove = useMutation({
    mutationFn: (name: string) => api.deleteHeadlessLog(serverId, slot, name),
    onSuccess: (_data, name) => {
      setLogError("");
      if (selected === name) {
        setSelected(null);
        setArchived("");
        setView(isRunning ? "live" : "file");
      }
      queryClient.invalidateQueries({ queryKey: ["headlessLogs", serverId, slot] });
    },
    onError: (err) => {
      setLogError(err instanceof Error ? err.message : "Failed to delete log");
    },
  });

  const download = useMutation({
    mutationFn: async (name: string | null) => {
      if (name) {
        await api.downloadHeadlessLog(serverId, slot, name);
        return;
      }
      const blob = new Blob([text || ""], { type: "text/plain;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `hc-${slot}-live-${new Date().toISOString().replace(/[:.]/g, "-")}.log`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    },
    onError: (err) => {
      setLogError(err instanceof Error ? err.message : "Failed to download log");
    },
  });

  const activeSessionName = activePath ? activePath.split(/[/\\]/).pop() : "";

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card className="lg:col-span-1">
        <CardHeader>
          <CardTitle className="text-base">HC logs</CardTitle>
          <CardDescription>
            RPT and session logs for {profileName}
          </CardDescription>
        </CardHeader>
        <CardContent className="max-h-96 overflow-y-auto">
          {logError && (
            <p className="mb-2 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
              {logError}
            </p>
          )}
          <ul className="text-sm">
            {isRunning && (
              <li>
                <button
                  type="button"
                  className={`mb-1 w-full rounded px-2 py-1 text-left hover:bg-accent ${
                    showingLive ? "bg-accent" : ""
                  }`}
                  onClick={() => setView("live")}
                >
                  <div className="font-medium">Live tail</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {activePath || "Streaming…"}
                  </div>
                </button>
              </li>
            )}
            {(logs.data ?? []).map((l) => {
              const isActiveLive =
                isRunning &&
                (l.name === activeSessionName ||
                  (activePath && activePath.endsWith(l.name)));
              return (
                <li
                  key={l.name}
                  className={`group flex items-start gap-1 rounded ${
                    !showingLive && selected === l.name ? "bg-accent" : ""
                  }`}
                >
                  <button
                    type="button"
                    className="min-w-0 flex-1 px-2 py-1 text-left hover:bg-accent/80"
                    onClick={() => loadArchived(l.name)}
                  >
                    <div className="truncate font-medium">{l.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {formatDate(l.modTime)} · {(l.size / 1024).toFixed(1)} KB
                      {isActiveLive ? " · active" : ""}
                    </div>
                  </button>
                  <div className="flex shrink-0 gap-0.5 py-1 pr-1 opacity-70 group-hover:opacity-100">
                    <Button
                      size="sm"
                      variant="ghost"
                      className="size-7 p-0"
                      title="Download"
                      disabled={download.isPending}
                      onClick={(e) => {
                        e.stopPropagation();
                        setLogError("");
                        download.mutate(l.name);
                      }}
                    >
                      <Download className="size-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="size-7 p-0"
                      title={
                        isActiveLive
                          ? "Stop the headless client to delete the active log"
                          : "Delete"
                      }
                      disabled={remove.isPending || !!isActiveLive}
                      onClick={(e) => {
                        e.stopPropagation();
                        if (!confirm(`Delete ${l.name}?`)) return;
                        remove.mutate(l.name);
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </li>
              );
            })}
            {!logs.data?.length && (
              <li className="text-muted-foreground">No logs yet.</li>
            )}
          </ul>
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="text-base">
              {showingLive ? "Live tail" : selected ? selected : "Log viewer"}
            </CardTitle>
            {showingLive && activePath && (
              <CardDescription className="truncate text-xs">{activePath}</CardDescription>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
              />
              Auto-scroll
            </label>
            <Button
              size="sm"
              variant="outline"
              disabled={download.isPending || (!showingLive && !selected)}
              onClick={() => {
                setLogError("");
                download.mutate(showingLive ? null : selected);
              }}
            >
              <Download className="size-3.5" /> Download
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <pre
            ref={logRef}
            className="h-[28rem] overflow-auto rounded-md bg-muted/40 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap"
          >
            {viewerText}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
