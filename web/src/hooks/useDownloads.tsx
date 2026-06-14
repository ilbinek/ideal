import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { QueueItem, QueueSnapshot } from "@/lib/types";

interface DownloadsContextValue {
  snapshot: QueueSnapshot;
  /** Find the live queue entry for a mod, if it is queued/downloading/finished. */
  itemFor: (modId: number) => QueueItem | undefined;
}

const empty: QueueSnapshot = {
  processing: false,
  active: 0,
  total: 0,
  done: 0,
  items: [],
};

const DownloadsContext = React.createContext<DownloadsContextValue>({
  snapshot: empty,
  itemFor: () => undefined,
});

export function useDownloads() {
  return React.useContext(DownloadsContext);
}

/**
 * Subscribes to the server-side download queue over SSE. State lives on the
 * server, so this reconnects cleanly after a reload and reflects in-progress
 * work immediately. Whenever a mod finishes (the queue's `done` count rises) we
 * invalidate mod queries so each row flips to its new state as it completes,
 * not only once the whole batch is finished.
 */
export function DownloadsProvider({ children }: { children: React.ReactNode }) {
  const [snapshot, setSnapshot] = React.useState<QueueSnapshot>(empty);
  const queryClient = useQueryClient();
  const lastDone = React.useRef(0);

  React.useEffect(() => {
    const source = new EventSource("/api/downloads/stream");
    source.onmessage = (e) => {
      try {
        const snap = JSON.parse(e.data) as QueueSnapshot;
        setSnapshot(snap);
        // `done` resets to 0 when a fresh batch is enqueued and rises by one as
        // each mod finishes (download or error). Any increase means at least one
        // mod's DB state just changed, so refetch the mod tables.
        if (snap.done > lastDone.current) {
          queryClient.invalidateQueries({ queryKey: ["mods"] });
        }
        lastDone.current = snap.done;
      } catch {
        /* ignore malformed events */
      }
    };
    source.onerror = () => {
      // EventSource auto-reconnects; nothing to do.
    };
    return () => source.close();
  }, [queryClient]);

  const itemFor = React.useCallback(
    (modId: number) => snapshot.items.find((i) => i.id === modId),
    [snapshot],
  );

  return (
    <DownloadsContext.Provider value={{ snapshot, itemFor }}>
      {children}
    </DownloadsContext.Provider>
  );
}
