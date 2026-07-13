export interface Mod {
  id: number;
  workshopId: string;
  name: string;
  downloaded: boolean;
  localPath: string;
  lastUpdated: string;
  lastChecked: string;
  updateAvailable: boolean;
}

export interface Modset {
  id: number;
  name: string;
  createdAt: string;
}

export type Downloader = "gosteam" | "depotdownloader" | "steamcmd";

export interface Settings {
  username: string;
  apiKey: string;
  hasPassword: boolean;
  downloader: Downloader;
  steamCmdPath: string;
  depotDownloaderPath: string;
  maxConcurrentDownloads: number;
}

export type SteamLoginPhase =
  | "idle"
  | "connecting"
  | "guard_required"
  | "qr_challenge"
  | "logged_in"
  | "error";

export interface SteamLoginState {
  phase: SteamLoginPhase;
  username?: string;
  guardKind?: "email" | "device" | "unknown";
  challengeUrl?: string;
  error?: string;
}

export type DownloadStatus = "queued" | "downloading" | "done" | "error";

export interface QueueItem {
  id: number;
  workshopId: string;
  name: string;
  status: DownloadStatus;
  percentage: number;
  message: string;
  fileName: string;
}

export interface QueueSnapshot {
  processing: boolean;
  active: number;
  total: number;
  done: number;
  items: QueueItem[];
}

export interface StartupCommand {
  command: string;
  keysPath: string;
  modPaths: string[];
}

export type DownloadScope = "all" | "outdated" | "missing";

export interface ArmaBranch {
  id: number;
  name: string;
  steamBranch: string;
  installPath: string;
  installed: boolean;
  lastUpdated: string;
}

export interface BranchInstallSnapshot {
  branchId: number;
  installing: boolean;
  message: string;
  percentage: number;
  error?: string;
}

export type ServerStatus = "stopped" | "starting" | "running" | "stopping" | "unknown";

export interface GameServer {
  id: number;
  name: string;
  slug: string;
  branchId: number;
  modsetId?: number;
  gamePort: number;
  steamPort: number;
  profileName: string;
  serverBinary: string;
  extraLaunchParams: string;
  status: ServerStatus;
  pid: number;
  activeRpt: string;
  createdAt: string;
  updatedAt: string;
}

export interface ServerMod {
  id?: number;
  serverId?: number;
  modId?: number;
  customPath?: string;
  label: string;
  isServerMod: boolean;
  sortOrder?: number;
}

export interface ServerKey {
  id: number;
  serverId: number;
  filename: string;
  sourcePath: string;
}

export interface ServerKeysResponse {
  base: string[];
  custom: ServerKey[];
  auto: string[];
}

export interface ServerStatusSnapshot {
  id: number;
  name: string;
  slug: string;
  status: ServerStatus;
  pid: number;
  activeRpt: string;
  uptime: number;
  startedAt?: string;
}

export interface AllServersSnapshot {
  servers: ServerStatusSnapshot[];
}

export interface LaunchArgs {
  command: string;
  modParam: string;
  serverModParam: string;
  binary: string;
  args: string[];
  workingDir: string;
}

export interface LogEntry {
  name: string;
  path: string;
  size: number;
  modTime: string;
}

export interface ProfileFileEntry {
  name: string;
  size: number;
  modTime: string;
}

export interface LogContent {
  content: string;
  totalSize: number;
  offset: number;
}
