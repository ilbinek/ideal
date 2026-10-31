export interface Mod {
  id: number;
  workshopId: string;
  name: string;
  downloaded: boolean;
  localPath: string;
  lastUpdated: string;
  lastChecked: string;
  updateAvailable: boolean;
  /** Last .pbo/.bisign/.bikey check of the current download; absent = not checked yet. */
  signature?: SignatureReport;
}

export type SignatureStatus = "ok" | "missing_key" | "unsigned" | "no_pbos" | "unknown";

export interface SignatureReport {
  status: SignatureStatus;
  pboCount: number;
  signedCount: number;
  unsignedPbos?: string[];
  unmatchedPbos?: string[];
  unmatchedCount?: number;
  authorities?: string[];
  keys?: string[];
  missingKeys?: string[];
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
  updateCheckIntervalMinutes: number;
  discordWebhookUrl: string;
  autoDownloadUpdates: boolean;
  useDocker: boolean;
  platform: string;
  dockerAvailable: boolean;
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

export interface SessionUser {
  id: number;
  steamId: string;
  displayName: string;
  avatarUrl: string;
}

export interface SessionPermissions {
  isFullAdmin: boolean;
  canManageMods: boolean;
  canManageBranches: boolean;
  serverIds: number[];
  canAccessServers: boolean;
  hasAnyAccess: boolean;
}

export interface Session {
  authenticated: boolean;
  subjectType?: "password_admin" | "user";
  user?: SessionUser;
  permissions?: SessionPermissions;
}

export interface AppUser {
  id: number;
  steamId: string;
  displayName: string;
  avatarUrl: string;
  isFullAdmin: boolean;
  canManageMods: boolean;
  canManageBranches: boolean;
  serverIds: number[];
  createdAt: string;
  lastLoginAt?: string | null;
}

export interface ArmaBranch {
  id: number;
  name: string;
  steamBranch: string;
  installPath: string;
  installed: boolean;
  lastUpdated: string;
  buildId: string;
  updateAvailable: boolean;
  lastChecked: string;
}

export interface BranchInstallSnapshot {
  branchId: number;
  installing: boolean;
  message: string;
  percentage: number;
  error?: string;
}

export type ServerStatus = "stopped" | "starting" | "running" | "stopping" | "unknown";

export interface ModServerRef {
  id: number;
  name: string;
  slug: string;
  status: ServerStatus;
}

export interface ModDetail extends Mod {
  modsets: Modset[];
  servers: ModServerRef[];
}

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
  enabledCdlcs: string[];
  headlessCount: number;
  missionHttpServe: boolean;
  missionHttpPath: string;
  sortOrder: number;
  /** Logical CPU indices (complete HT pairs). Empty = no pinning. */
  cpuAffinity: number[];
  /** Shared RAM cap for dedicated + headless (MB). 0 = unlimited. */
  memoryLimitMB: number;
  status: ServerStatus;
  pid: number;
  activeRpt: string;
  createdAt: string;
  updatedAt: string;
}

export interface CPUTopology {
  logicalCount: number;
  pairs: number[][];
}

export interface CdlcInfo {
  code: string;
  name: string;
  available: boolean;
}

export interface CdlcCatalog {
  installed: boolean;
  items: CdlcInfo[];
}

export interface ServerMod {
  id?: number;
  serverId?: number;
  modId?: number;
  customPath?: string;
  label: string;
  isServerMod: boolean;
  /** Key-only: the mod's .bikey is added to the server but the mod is never loaded. */
  optional?: boolean;
  enabled?: boolean;
  source?: string;
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

export interface HeadlessClientSnapshot {
  serverId: number;
  slot: number;
  profileName: string;
  status: ServerStatus;
  pid: number;
  activeRpt: string;
  uptime: number;
  startedAt?: string;
  metrics?: ProcessMetrics;
}

export interface HeadlessClientsResponse {
  count: number;
  clients: HeadlessClientSnapshot[];
}

export interface ProcessMetrics {
  /** Process CPU usage over the last sample interval; may exceed 100% on multi-core. */
  cpuPercent: number;
  /** Logical CPU count (for gauge scale: full = 100% × cpuCount). */
  cpuCount: number;
  /** Overall host CPU utilization 0–100 (all cores averaged). */
  hostCpuPercent: number;
  memoryBytes: number;
  /** Host memory currently in use (all processes). */
  memoryUsedBytes: number;
  /** Host physical/virtual memory total for RAM gauge. */
  memoryTotalBytes: number;
  threads: number;
  /** Host aggregate NIC receive throughput (bytes/sec). */
  netRecvBytesPerSec: number;
  /** Host aggregate NIC transmit throughput (bytes/sec). */
  netSentBytesPerSec: number;
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
  metrics?: ProcessMetrics;
  headless?: HeadlessClientSnapshot[];
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
  nextOffset?: number;
  truncated?: boolean;
}
