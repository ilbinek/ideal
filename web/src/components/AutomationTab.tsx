import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Loader2, Plus, Trash2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import {
  ALL_DAYS,
  currentUtcOffset,
  formatMinute,
  localToSlot,
  parseMinute,
  parseWarnMinutes,
  slotToLocal,
  type LocalSlot,
} from "@/lib/schedule";
import type { ServerEvent } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useToast } from "@/components/ui/toast";

const dayNames = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
// Monday first, while bit positions stay Sunday-based.
const dayOrder = [1, 2, 3, 4, 5, 6, 0];

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

function ScheduleRow({
  slot,
  onChange,
  onRemove,
}: {
  slot: LocalSlot;
  onChange: (slot: LocalSlot) => void;
  onRemove: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        type="time"
        aria-label="Restart time"
        className="w-28"
        value={formatMinute(slot.minute)}
        onChange={(e) => {
          const minute = parseMinute(e.target.value);
          if (minute !== null) onChange({ ...slot, minute });
        }}
      />
      <div className="flex flex-wrap gap-1">
        {dayOrder.map((day) => {
          const on = (slot.days & (1 << day)) !== 0;
          return (
            <Button
              key={day}
              type="button"
              size="sm"
              variant={on ? "default" : "outline"}
              aria-pressed={on}
              className="px-2"
              onClick={() => onChange({ ...slot, days: slot.days ^ (1 << day) })}
            >
              {dayNames[day]}
            </Button>
          );
        })}
      </div>
      <Button type="button" size="sm" variant="ghost" aria-label="Remove this time" onClick={onRemove}>
        <Trash2 className="size-3.5" />
      </Button>
    </div>
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
  const [warnText, setWarnText] = useState("");
  const [slots, setSlots] = useState<LocalSlot[]>([]);

  // Only when the saved settings really changed: the RCON card refreshes this same
  // query, and that mustn't throw away edits that haven't been saved yet.
  const savedSettings = data
    ? JSON.stringify([
        data.autostart,
        data.restartOnCrash,
        data.scheduleEnabled,
        data.warnMinutes,
        data.restartSlots,
      ])
    : null;
  useEffect(() => {
    if (data) {
      setAutostart(data.autostart);
      setRestartOnCrash(data.restartOnCrash);
      setScheduleEnabled(data.scheduleEnabled);
      setWarnText(data.warnMinutes.join(", "));
      const offset = currentUtcOffset();
      setSlots(data.restartSlots.map((s) => slotToLocal(s, offset)));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [savedSettings]);

  const save = useMutation({
    mutationFn: () => {
      const warnMinutes = parseWarnMinutes(warnText);
      if (warnMinutes === null) {
        return Promise.reject(new ApiError(400, "Warnings must be whole minutes, separated by commas"));
      }
      const offset = currentUtcOffset();
      return api.saveServerAutomation(serverId, {
        autostart,
        restartOnCrash,
        scheduleEnabled,
        warnMinutes,
        restartSlots: slots.map((s) => localToSlot(s, offset)),
      });
    },
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

  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

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
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Planned restarts</CardTitle>
          <CardDescription>
            Restarts this server at set times, in your browser's timezone ({timeZone}). A schedule never
            starts a stopped server, and skips one started less than 10 minutes earlier.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 text-sm">
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              className="mt-1"
              checked={scheduleEnabled}
              onChange={(e) => setScheduleEnabled(e.target.checked)}
            />
            <span className="font-medium">Restart on a schedule</span>
          </label>
          <div className="flex flex-col gap-3">
            {slots.map((slot, i) => (
              <ScheduleRow
                key={i}
                slot={slot}
                onChange={(next) => setSlots(slots.map((s, j) => (j === i ? next : s)))}
                onRemove={() => setSlots(slots.filter((_, j) => j !== i))}
              />
            ))}
            <div>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() =>
                  setSlots([
                    ...slots,
                    { minute: 360, days: slots.length > 0 ? slots[slots.length - 1].days : ALL_DAYS },
                  ])
                }
              >
                <Plus className="size-3.5" /> Add time
              </Button>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            Times are stored in UTC, so a time here moves by an hour when daylight saving starts or ends.
          </p>
          <div className="flex max-w-xs flex-col gap-2">
            <Label htmlFor="auto-warn">Warn players (minutes before)</Label>
            <Input
              id="auto-warn"
              placeholder="15, 5, 1"
              value={warnText}
              onChange={(e) => setWarnText(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Needs BattlEye RCON. Without it, the server restarts with no warning.
            </p>
          </div>
        </CardContent>
      </Card>

      <div>
        <Button type="button" disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending && <Loader2 className="size-4 animate-spin" />}
          Save automation
        </Button>
      </div>

      <RConCard serverId={serverId} />
      <EventHistory serverId={serverId} />
    </div>
  );
}
