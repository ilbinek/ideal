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

export interface Settings {
  username: string;
  apiKey: string;
  hasPassword: boolean;
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
