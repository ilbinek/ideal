import type {
  Downloader,
  DownloadScope,
  Mod,
  Modset,
  QueueSnapshot,
  Settings,
  StartupCommand,
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
  }) =>
    request<{ ok: boolean }>("/api/settings", {
      method: "POST",
      body: JSON.stringify(s),
    }),

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

  // Mods
  getMods: (modset?: number) =>
    request<Mod[]>(`/api/mods${modset ? `?modset=${modset}` : ""}`),
  addMod: (workshopId: string, modsetId: number | null) =>
    request<{ workshopId: string; name: string }>("/api/mods", {
      method: "POST",
      body: JSON.stringify({ workshopId, modsetId }),
    }),
  deleteMod: (id: number) =>
    request<{ ok: boolean }>(`/api/mods/${id}`, { method: "DELETE" }),
  checkUpdates: () =>
    request<Record<string, boolean>>("/api/mods/check-updates", {
      method: "POST",
    }),
  downloadMod: (id: number) =>
    request<{ status: string }>(`/api/mods/${id}/download`, { method: "POST" }),

  // Download queue
  enqueue: (scope: DownloadScope, modset?: number) =>
    request<{ enqueued: number }>("/api/downloads/enqueue", {
      method: "POST",
      body: JSON.stringify({ scope, modset: modset ?? null }),
    }),
  downloadStatus: () => request<QueueSnapshot>("/api/downloads/status"),

  // A3Sync + startup
  generateA3Sync: () =>
    request<{ ok: boolean }>("/api/a3sync/generate", { method: "POST" }),
  startupCommand: (modset: number) =>
    request<StartupCommand>(`/api/startup-command?modset=${modset}`),
};
