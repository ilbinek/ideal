import * as React from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import type {
  AllServersSnapshot,
  GameServer,
  ServerStatus,
  ServerStatusSnapshot,
} from "@/lib/types";

const empty: AllServersSnapshot = { servers: [] };

const ServersContext = React.createContext<{
  snapshot: AllServersSnapshot;
  statusFor: (id: number) => ServerStatusSnapshot | undefined;
}>({
  snapshot: empty,
  statusFor: () => undefined,
});

export function useServers() {
  return React.useContext(ServersContext);
}

export function patchServerStatus(
  queryClient: QueryClient,
  serverId: number,
  status: ServerStatus,
) {
  queryClient.setQueryData<GameServer>(["servers", serverId], (old) =>
    old ? { ...old, status } : old,
  );
  queryClient.setQueryData<GameServer[]>(["servers"], (old) =>
    old?.map((s) => (s.id === serverId ? { ...s, status } : s)),
  );
}

function applySnapshotToCache(
  queryClient: QueryClient,
  snap: AllServersSnapshot,
) {
  const liveById = new Map(snap.servers.map((s) => [s.id, s]));

  queryClient.setQueryData<GameServer[]>(["servers"], (old) => {
    if (!old) return old;
    return old.map((srv) => {
      const live = liveById.get(srv.id);
      if (!live) return srv;
      return {
        ...srv,
        status: live.status,
        pid: live.pid,
        activeRpt: live.activeRpt,
      };
    });
  });

  for (const live of snap.servers) {
    queryClient.setQueryData<GameServer>(["servers", live.id], (old) => {
      if (!old) return old;
      return {
        ...old,
        status: live.status,
        pid: live.pid,
        activeRpt: live.activeRpt,
      };
    });
  }
}

export function ServersProvider({ children }: { children: React.ReactNode }) {
  const [snapshot, setSnapshot] = React.useState<AllServersSnapshot>(empty);
  const queryClient = useQueryClient();

  React.useEffect(() => {
    const source = new EventSource("/api/servers/stream");
    source.onmessage = (e) => {
      try {
        const snap = JSON.parse(e.data) as AllServersSnapshot;
        setSnapshot(snap);
        applySnapshotToCache(queryClient, snap);
      } catch {
        /* ignore */
      }
    };
    return () => source.close();
  }, [queryClient]);

  const statusFor = React.useCallback(
    (id: number) => snapshot.servers.find((s) => s.id === id),
    [snapshot],
  );

  return (
    <ServersContext.Provider value={{ snapshot, statusFor }}>
      {children}
    </ServersContext.Provider>
  );
}
