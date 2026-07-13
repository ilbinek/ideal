import type {
  ArmaBranch,
  Downloader,
  DownloadScope,
  GameServer,
  HeadlessClientsResponse,
  LaunchArgs,
  LogContent,
  LogEntry,
  Mod,
  ModDetail,
  Modset,
  ProfileFileEntry,
  QueueSnapshot,
  ServerKeysResponse,
  ServerMod,
  ServerStatusSnapshot,
  Settings,
  StartupCommand,
  SteamLoginState,
} from "./types";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** Set by the app so a 401 anywhere can bounce the user to the login screen. */
let unauthorizedHandler: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  unauthorizedHandler = fn;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });

  if (res.status === 401) {
    unauthorizedHandler?.();
    throw new ApiError(401, "unauthorized");
  }

  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      /* ignore non-JSON error bodies */
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

export const api = {
  // Auth
  session: () => request<{ authenticated: boolean }>("/api/session"),
  login: (password: string) =>
    request<{ ok: boolean }>("/api/login", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  logout: () => request<{ ok: boolean }>("/api/logout", { method: "POST" }),

  // Settings
  getSettings: () => request<Settings>("/api/settings"),
  saveSettings: (s: {
    username: string;
    password: string;
    apiKey: string;
    downloader: Downloader;
    steamCmdPath: string;
    depotDownloaderPath: string;
    maxConcurrentDownloads: number;
  }) =>
    request<{ ok: boolean }>("/api/settings", {
      method: "POST",
      body: JSON.stringify(s),
    }),

  // Steam login (built-in gosteam downloader)
  steamStatus: () => request<SteamLoginState>("/api/steam/status"),
  steamLogin: (body: { method: "credentials" | "qr"; username?: string; password?: string }) =>
    request<{ started: boolean }>("/api/steam/login", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  steamGuard: (code: string) =>
    request<{ ok: boolean }>("/api/steam/login/guard", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
  steamLogout: () => request<{ ok: boolean }>("/api/steam/logout", { method: "POST" }),

  // Modsets
  getModsets: () => request<Modset[]>("/api/modsets"),
  uploadModset: (file: File) => {
    const form = new FormData();
    form.append("modset", file);
    return request<{ modsetId: number; name: string; count: number }>(
      "/api/modsets/upload",
      { method: "POST", body: form, headers: {} },
    );
  },
  deleteModset: (id: number) =>
    request<{ ok: boolean }>(`/api/modsets/${id}`, { method: "DELETE" }),
  removeModFromModset: (modsetId: number, modId: number) =>
    request<{ ok: boolean }>(`/api/modsets/${modsetId}/mods/${modId}`, {
      method: "DELETE",
    }),

  // Mods
  getMods: (modset?: number) =>
    request<Mod[]>(`/api/mods${modset ? `?modset=${modset}` : ""}`),
  getMod: (id: number) => request<ModDetail>(`/api/mods/${id}`),
  addMod: (workshopId: string, modsetId: number | null) =>
    request<{ workshopId: string; name: string }>("/api/mods", {
      method: "POST",
      body: JSON.stringify({ workshopId, modsetId }),
    }),
  deleteMod: (id: number) =>
    request<{ ok: boolean }>(`/api/mods/${id}`, { method: "DELETE" }),
  bulkDeleteMods: (ids: number[]) =>
    request<{ deleted: number; failed: number[] }>("/api/mods/bulk-delete", {
      method: "POST",
      body: JSON.stringify({ ids }),
    }),
  checkUpdates: (modset?: number) =>
    request<Record<string, boolean>>("/api/mods/check-updates", {
      method: "POST",
      body: JSON.stringify({ modset: modset ?? null }),
    }),
  downloadMod: (id: number) =>
    request<{ status: string }>(`/api/mods/${id}/download`, { method: "POST" }),

  // Download queue
  enqueue: (scope: DownloadScope, modset?: number) =>
    request<{ enqueued: number }>("/api/downloads/enqueue", {
      method: "POST",
      body: JSON.stringify({ scope, modset: modset ?? null }),
    }),
  enqueueIds: (ids: number[]) =>
    request<{ enqueued: number }>("/api/downloads/enqueue", {
      method: "POST",
      body: JSON.stringify({ ids }),
    }),
  downloadStatus: () => request<QueueSnapshot>("/api/downloads/status"),

  // A3Sync + startup
  generateA3Sync: () =>
    request<{ ok: boolean }>("/api/a3sync/generate", { method: "POST" }),
  startupCommand: (modset: number) =>
    request<StartupCommand>(`/api/startup-command?modset=${modset}`),

  // Branches
  getBranches: () => request<ArmaBranch[]>("/api/branches"),
  createBranch: (name: string, steamBranch: string) =>
    request<{ id: number }>("/api/branches", {
      method: "POST",
      body: JSON.stringify({ name, steamBranch }),
    }),
  deleteBranch: (id: number) =>
    request<{ ok: boolean }>(`/api/branches/${id}`, { method: "DELETE" }),
  installBranch: (id: number) =>
    request<{ ok: boolean }>(`/api/branches/${id}/install`, { method: "POST" }),
  getBranchBinaries: (id: number) =>
    request<{ binaries: string[]; preferred: string }>(`/api/branches/${id}/binaries`),

  // Servers
  getServers: () => request<GameServer[]>("/api/servers"),
  createServer: (body: {
    name: string;
    branchId: number;
    modsetId?: number | null;
    gamePort?: number;
    steamPort?: number;
    profileName?: string;
    serverBinary?: string;
  }) =>
    request<{ id: number; slug: string }>("/api/servers", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  getServer: (id: number) => request<GameServer>(`/api/servers/${id}`),
  updateServer: (
    id: number,
    body: {
      name: string;
      branchId: number;
      modsetId?: number | null;
      gamePort: number;
      steamPort: number;
      profileName: string;
      serverBinary: string;
      extraLaunchParams: string;
    },
  ) =>
    request<{ ok: boolean }>(`/api/servers/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  discoverLocalMods: (path: string) =>
    request<{ mods: { name: string; path: string }[] }>("/api/localmods/discover", {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  deleteServer: (id: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}`, { method: "DELETE" }),
  provisionServer: (id: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/provision`, { method: "POST" }),
  startServer: (id: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/start`, { method: "POST" }),
  stopServer: (id: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/stop`, { method: "POST" }),
  restartServer: (id: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/restart`, { method: "POST" }),
  getServerStatus: (id: number) =>
    request<ServerStatusSnapshot>(`/api/servers/${id}/status`),
  getLaunchPreview: (id: number) =>
    request<LaunchArgs>(`/api/servers/${id}/launch`),

  // Server mods
  getServerMods: (id: number) => request<ServerMod[]>(`/api/servers/${id}/mods`),
  putServerMods: (id: number, mods: ServerMod[]) =>
    request<{ ok: boolean }>(`/api/servers/${id}/mods`, {
      method: "PUT",
      body: JSON.stringify(mods),
    }),

  // Server keys
  getServerKeys: (id: number) => request<ServerKeysResponse>(`/api/servers/${id}/keys`),
  rescanServerKeys: (id: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/keys/rescan`, { method: "POST" }),
  uploadServerKey: (id: number, file: File) => {
    const form = new FormData();
    form.append("key", file);
    return request<{ id: number }>(`/api/servers/${id}/keys`, {
      method: "POST",
      body: form,
      headers: {},
    });
  },
  deleteServerKey: (serverId: number, keyId: number) =>
    request<{ ok: boolean }>(`/api/servers/${serverId}/keys/${keyId}`, {
      method: "DELETE",
    }),

  // Server config
  getServerConfig: (id: number, name: string) =>
    request<{ content: string }>(`/api/servers/${id}/config/${name}`),
  putServerConfig: (id: number, name: string, content: string) =>
    request<{ ok: boolean }>(`/api/servers/${id}/config/${name}`, {
      method: "PUT",
      body: JSON.stringify({ content }),
    }),

  listProfileFiles: (id: number) =>
    request<ProfileFileEntry[]>(`/api/servers/${id}/profile/files`),
  getProfileFile: (id: number, filename: string) =>
    request<{ content: string }>(
      `/api/servers/${id}/profile/${encodeURIComponent(filename)}`,
    ),
  putProfileFile: (id: number, filename: string, content: string) =>
    request<{ ok: boolean }>(
      `/api/servers/${id}/profile/${encodeURIComponent(filename)}`,
      { method: "PUT", body: JSON.stringify({ content }) },
    ),

  // Logs
  listServerLogs: (id: number) => request<LogEntry[]>(`/api/servers/${id}/logs`),
  getServerLog: (id: number, filename: string, offset?: number) =>
    request<LogContent>(
      `/api/servers/${id}/logs/${encodeURIComponent(filename)}${offset != null ? `?offset=${offset}` : ""}`,
    ),
  deleteServerLog: (id: number, filename: string) =>
    request<{ ok: boolean }>(`/api/servers/${id}/logs/${encodeURIComponent(filename)}`, {
      method: "DELETE",
    }),
  downloadServerLog: async (id: number, filename: string) => {
    const res = await fetch(`/api/servers/${id}/logs/${encodeURIComponent(filename)}/download`);
    if (res.status === 401) {
      unauthorizedHandler?.();
      throw new ApiError(401, "unauthorized");
    }
    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        if (body?.error) message = body.error;
      } catch {
        /* ignore */
      }
      throw new ApiError(res.status, message);
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  },

  // Headless clients
  getHeadless: (id: number) =>
    request<HeadlessClientsResponse>(`/api/servers/${id}/headless`),
  setHeadlessCount: (id: number, count: number) =>
    request<HeadlessClientsResponse>(`/api/servers/${id}/headless`, {
      method: "PUT",
      body: JSON.stringify({ count }),
    }),
  startHeadless: (id: number, slot: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/headless/${slot}/start`, {
      method: "POST",
    }),
  stopHeadless: (id: number, slot: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/headless/${slot}/stop`, {
      method: "POST",
    }),
  restartHeadless: (id: number, slot: number) =>
    request<{ ok: boolean }>(`/api/servers/${id}/headless/${slot}/restart`, {
      method: "POST",
    }),
  listHeadlessLogs: (id: number, slot: number) =>
    request<LogEntry[]>(`/api/servers/${id}/headless/${slot}/logs`),
  getHeadlessLog: (id: number, slot: number, filename: string, offset?: number) =>
    request<LogContent>(
      `/api/servers/${id}/headless/${slot}/logs/${encodeURIComponent(filename)}${
        offset != null ? `?offset=${offset}` : ""
      }`,
    ),
  deleteHeadlessLog: (id: number, slot: number, filename: string) =>
    request<{ ok: boolean }>(
      `/api/servers/${id}/headless/${slot}/logs/${encodeURIComponent(filename)}`,
      { method: "DELETE" },
    ),
  downloadHeadlessLog: async (id: number, slot: number, filename: string) => {
    const res = await fetch(
      `/api/servers/${id}/headless/${slot}/logs/${encodeURIComponent(filename)}/download`,
    );
    if (res.status === 401) {
      unauthorizedHandler?.();
      throw new ApiError(401, "unauthorized");
    }
    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        if (body?.error) message = body.error;
      } catch {
        /* ignore */
      }
      throw new ApiError(res.status, message);
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  },

  // Missions
  listMissions: (id: number) => request<LogEntry[]>(`/api/servers/${id}/missions`),
  uploadMission: (id: number, file: File) => {
    const form = new FormData();
    form.append("mission", file);
    return request<{ name: string }>(`/api/servers/${id}/missions/upload`, {
      method: "POST",
      body: form,
      headers: {},
    });
  },
  deleteMission: (id: number, name: string) =>
    request<{ ok: boolean }>(`/api/servers/${id}/missions/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),
};
