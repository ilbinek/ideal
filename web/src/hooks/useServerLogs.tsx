import * as React from "react";

export function useServerLogs(serverId: number, enabled: boolean) {
  const [lines, setLines] = React.useState<string[]>([]);
  const [activePath, setActivePath] = React.useState("");

  React.useEffect(() => {
    if (!enabled || !serverId) return;
    setLines([]);
    const source = new EventSource(`/api/servers/${serverId}/logs/stream`);
    source.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data) as { chunk: string; path: string };
        if (data.path) setActivePath(data.path);
        if (data.chunk) {
          setLines((prev) => {
            const next = [...prev, data.chunk];
            if (next.length > 500) return next.slice(-400);
            return next;
          });
        }
      } catch {
        /* ignore */
      }
    };
    return () => source.close();
  }, [serverId, enabled]);

  return { lines, activePath, text: lines.join("") };
}

export function useBranchInstall() {
  const [snapshot, setSnapshot] = React.useState({
    branchId: 0,
    installing: false,
    message: "",
    percentage: 0,
    error: "",
  });

  React.useEffect(() => {
    const source = new EventSource("/api/branches/install/stream");
    source.onmessage = (e) => {
      try {
        setSnapshot(JSON.parse(e.data));
      } catch {
        /* ignore */
      }
    };
    return () => source.close();
  }, []);

  return snapshot;
}
