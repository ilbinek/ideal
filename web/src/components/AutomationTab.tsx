import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Loader2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { RestartSlot, ServerEvent } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useToast } from "@/components/ui/toast";

const eventLabels: Record<string, string> = {
  started: "Started",
  start_failed: "Start failed",
  stopped: "Stopped",
  stop_failed: "Stop failed",
  force_stop: "Force stop requested",
  restarted: "Restarted",
  exited: "Exited unexpectedly",
  retry_scheduled: "Restart scheduled",
  gave_up: "Gave up restarting",
  planned_restart: "Planned restart",
  planned_skipped: "Planned restart skipped",
  planned_missed: "Planned restart missed",
  warning_sent: "Warning sent to players",
  boot_start: "Started on launch",
  content_changed: "Mods or game files changed",
  identity_unknown: "Process could not be verified",
  port_held: "Game port in use",
};

const problemEvents = new Set([
  "start_failed",
  "stop_failed",
  "exited",
  "gave_up",
  "identity_unknown",
  "port_held",
]);

function errorMessage(e: unknown): string | undefined {
  return e instanceof ApiError ? e.message : undefined;
}

function EventHistory({ serverId }: { serverId: number }) {
  const [older, setOlder] = useState<ServerEvent[]>([]);
  const [exhausted, setExhausted] = useState(false);
  const pageSize = 50;

  const latest = useQuery({
    queryKey: ["servers", serverId, "events"],
    queryFn: () => api.getServerEvents(serverId, pageSize),
    refetchInterval: 15000,
  });

  const loadMore = useMutation({
    mutationFn: (before: number) => api.getServerEvents(serverId, pageSize, before),
    onSuccess: (page) => {
      setOlder((prev) => [...prev, ...page]);
      if (page.length < pageSize) setExhausted(true);
    },
  });

  const events = [...(latest.data ?? []), ...older];
  const canLoadMore = !exhausted && (latest.data?.length ?? 0) >= pageSize;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">History</CardTitle>
        <CardDescription>
          Starts, stops and anything Ideal did on its own. Keeps the latest 500 entries.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {latest.isLoading ? (
          <Loader2 className="size-4 animate-spin" />
        ) : events.length === 0 ? (
          <p className="text-sm text-muted-foreground">Nothing has happened yet.</p>
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {events.map((e) => (
              <li key={e.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 py-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground">
                  {new Date(e.at).toLocaleString()}
                </span>
                <span className={problemEvents.has(e.kind) ? "font-medium text-destructive" : "font-medium"}>
                  {eventLabels[e.kind] ?? e.kind}
                </span>
                {e.actor && e.actor !== "system" && (
                  <span className="text-xs text-muted-foreground">by {e.actor}</span>
                )}
                {e.detail && (
                  <span className="basis-full text-xs break-words text-muted-foreground">{e.detail}</span>
                )}
              </li>
            ))}
          </ul>
        )}
        {canLoadMore && (
          <div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={loadMore.isPending}
              onClick={() => loadMore.mutate(events[events.length - 1].id)}
            >
              Show older
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function RConCard({ serverId }: { serverId: number }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["servers", serverId, "automation"],
    queryFn: () => api.getServerAutomation(serverId),
  });
  const generate = useMutation({
    mutationFn: () => api.generateRCon(serverId),
    onSuccess: (result) => {
      toast.success(
        "RCON settings saved",
        result.requiresRestart ? "Restart the server for them to take effect." : undefined,
      );
      queryClient.invalidateQueries({ queryKey: ["servers", serverId, "automation"] });
    },
    onError: (e) => toast.error("Could not generate RCON settings", errorMessage(e)),
  });

  const rcon = data?.rcon;
  const usable = !!rcon?.configured && !!rcon?.battleyeEnabled;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Graceful shutdown</CardTitle>
        <CardDescription>
          With BattlEye RCON, Ideal shuts the server down with #shutdown and can warn players before a
          planned restart. Without it, Ideal asks the operating system to close the server, and players
          get no warning.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={usable ? "success" : "outline"}>
            {usable ? "RCON ready" : "RCON not available"}
          </Badge>
          {rcon?.configured && (
            <span className="font-mono text-xs text-muted-foreground">
              port {rcon.port} · {rcon.source}
            </span>
          )}
        </div>
        {rcon && !rcon.battleyeEnabled && (
          <p className="text-xs text-muted-foreground">
            BattlEye is turned off in server.cfg, so RCON won't be used.
          </p>
        )}
        {usable && rcon?.restrictRcon !== "0" && (
          <p className="text-xs text-destructive">
            RestrictRCon is {rcon?.restrictRcon === "1" ? "set to 1" : "not set"} in {rcon?.source}, which
            may block the shutdown command. Set it to 0 if planned restarts fall back to closing the server.
          </p>
        )}
        {rcon?.battleyeEnabled && !rcon.configured && (
          <div className="flex flex-col items-start gap-2">
            <p className="text-xs text-muted-foreground">
              Generate adds only the missing settings to this server's BattlEye config, and never changes
              values already there. It takes effect the next time the server starts.
            </p>
            <Button type="button" size="sm" disabled={generate.isPending} onClick={() => generate.mutate()}>
              {generate.isPending ? <Loader2 className="size-4 animate-spin" /> : <KeyRound className="size-3.5" />}
              Generate RCON settings
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function AutomationTab({ serverId }: { serverId: number }) {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["servers", serverId, "automation"],
    queryFn: () => api.getServerAutomation(serverId),
  });

  const [autostart, setAutostart] = useState(false);
  const [restartOnCrash, setRestartOnCrash] = useState(false);
  const [scheduleEnabled, setScheduleEnabled] = useState(false);
  const [warnMinutes, setWarnMinutes] = useState<number[]>([]);
  const [slots, setSlots] = useState<RestartSlot[]>([]);

  useEffect(() => {
    if (data) {
      setAutostart(data.autostart);
      setRestartOnCrash(data.restartOnCrash);
      setScheduleEnabled(data.scheduleEnabled);
      setWarnMinutes(data.warnMinutes);
      setSlots(data.restartSlots);
    }
  }, [data]);

  const save = useMutation({
    mutationFn: () =>
      api.saveServerAutomation(serverId, {
        autostart,
        restartOnCrash,
        scheduleEnabled,
        warnMinutes,
        restartSlots: slots,
      }),
    onSuccess: (saved) => {
      toast.success("Automation saved");
      queryClient.setQueryData(["servers", serverId, "automation"], saved);
    },
    onError: (e) => toast.error("Failed to save", errorMessage(e)),
  });

  if (!data) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 className="size-6 animate-spin" />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Autostart</CardTitle>
          <CardDescription>
            Each setting works on its own. To come back after a power loss, a server needs "Start when
            Ideal launches".
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 text-sm">
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              className="mt-1"
              checked={autostart}
              onChange={(e) => setAutostart(e.target.checked)}
            />
            <span>
              <span className="font-medium">Start when Ideal launches</span>
              <span className="mt-1 block text-xs text-muted-foreground">
                Starts this server whenever Ideal starts, unless someone stopped it on purpose.
              </span>
            </span>
          </label>
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              className="mt-1"
              checked={restartOnCrash}
              onChange={(e) => setRestartOnCrash(e.target.checked)}
            />
            <span>
              <span className="font-medium">Restart if it exits unexpectedly</span>
              <span className="mt-1 block text-xs text-muted-foreground">
                Restarts after any exit not requested through Ideal, including #shutdown typed in-game. Stops
                trying after 5 failures in 15 minutes. When this is off, Ideal never restarts the server by
                itself.
              </span>
            </span>
          </label>
          {data.userStopped && autostart && (
            <p className="text-xs text-muted-foreground">
              This server was stopped on purpose, so it won't start when Ideal launches until someone starts it.
            </p>
          )}
          {data.gaveUpAt > 0 && (
            <p className="text-xs text-destructive">
              Ideal gave up restarting this server on {new Date(data.gaveUpAt * 1000).toLocaleString()}. It stays
              off until someone starts it.
            </p>
          )}
          <div>
            <Button type="button" disabled={save.isPending} onClick={() => save.mutate()}>
              {save.isPending && <Loader2 className="size-4 animate-spin" />}
              Save automation
            </Button>
          </div>
        </CardContent>
      </Card>

      <RConCard serverId={serverId} />
      <EventHistory serverId={serverId} />
    </div>
  );
}
