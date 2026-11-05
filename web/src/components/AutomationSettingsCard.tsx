import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
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

/** Global automation settings. Saves separately from the rest of the Settings page. */
export function AutomationSettingsCard() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["settings", "automation"],
    queryFn: () => api.getAutomationSettings(),
  });

  const [paused, setPaused] = useState(false);
  const [bootDelaySec, setBootDelaySec] = useState(30);
  const [bootGapSec, setBootGapSec] = useState(30);
  const [warningTemplate, setWarningTemplate] = useState("");

  useEffect(() => {
    if (data) {
      setPaused(data.paused);
      setBootDelaySec(data.bootDelaySec);
      setBootGapSec(data.bootGapSec);
      setWarningTemplate(data.warningTemplate);
    }
  }, [data]);

  const save = useMutation({
    mutationFn: () =>
      api.saveAutomationSettings({ paused, bootDelaySec, bootGapSec, warningTemplate }),
    onSuccess: (saved) => {
      toast.success("Automation settings saved");
      queryClient.setQueryData(["settings", "automation"], saved);
    },
    onError: (e) =>
      toast.error("Failed to save", e instanceof ApiError ? e.message : undefined),
  });

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="text-base">Server automation</CardTitle>
        <CardDescription>
          Autostart and restart schedules are set per server, on its Automation tab.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        <label className="flex items-start gap-2">
          <input
            type="checkbox"
            className="mt-1"
            checked={paused}
            onChange={(e) => setPaused(e.target.checked)}
          />
          <span>
            <span className="font-medium">Pause all automation</span>
            <span className="mt-1 block text-xs text-muted-foreground">
              For maintenance. No automatic starts, restarts or planned restarts, and pending ones are
              cancelled rather than caught up on when you unpause.
            </span>
          </span>
        </label>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-2">
            <Label htmlFor="auto-boot-delay">Delay after launch (seconds)</Label>
            <Input
              id="auto-boot-delay"
              type="number"
              min={0}
              max={3600}
              value={bootDelaySec}
              onChange={(e) => setBootDelaySec(Number(e.target.value) || 0)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="auto-boot-gap">Gap between servers (seconds)</Label>
            <Input
              id="auto-boot-gap"
              type="number"
              min={0}
              max={3600}
              value={bootGapSec}
              onChange={(e) => setBootGapSec(Number(e.target.value) || 0)}
            />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="auto-warning">Planned restart warning</Label>
          <Input
            id="auto-warning"
            maxLength={150}
            value={warningTemplate}
            onChange={(e) => setWarningTemplate(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            Shown to players over RCON. {"{minutes}"} becomes the minutes left.
          </p>
        </div>
        <div>
          <Button type="button" disabled={save.isPending || !data} onClick={() => save.mutate()}>
            {save.isPending && <Loader2 className="size-4 animate-spin" />}
            Save automation settings
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
