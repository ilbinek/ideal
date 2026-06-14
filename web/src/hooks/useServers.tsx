import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { AllServersSnapshot, ServerStatusSnapshot } from "@/lib/types";

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

export function ServersProvider({ children }: { children: React.ReactNode }) {
  const [snapshot, setSnapshot] = React.useState<AllServersSnapshot>(empty);
  const queryClient = useQueryClient();

  React.useEffect(() => {
    const source = new EventSource("/api/servers/stream");
    source.onmessage = (e) => {
      try {
        const snap = JSON.parse(e.data) as AllServersSnapshot;
        setSnapshot(snap);
        queryClient.invalidateQueries({ queryKey: ["servers"] });
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
