import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { Downloader } from "@/lib/types";
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
import { Select } from "@/components/ui/select";
import { useToast } from "@/components/ui/toast";
import { SteamLogin } from "@/components/SteamLogin";
import { PageHeader } from "@/components/PageHeader";

export function SettingsPage() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: () => api.getSettings() });

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [downloader, setDownloader] = useState<Downloader>("gosteam");
  const [steamCmdPath, setSteamCmdPath] = useState("");
  const [depotDownloaderPath, setDepotDownloaderPath] = useState("");
  const [maxConcurrentDownloads, setMaxConcurrentDownloads] = useState(4);
  const [updateCheckIntervalMinutes, setUpdateCheckIntervalMinutes] = useState(0);
  const [discordWebhookUrl, setDiscordWebhookUrl] = useState("");

  useEffect(() => {
    if (data) {
      setUsername(data.username);
      setApiKey(data.apiKey);
      setDownloader(data.downloader);
      setSteamCmdPath(data.steamCmdPath);
      setDepotDownloaderPath(data.depotDownloaderPath);
      setMaxConcurrentDownloads(data.maxConcurrentDownloads || 4);
      setUpdateCheckIntervalMinutes(data.updateCheckIntervalMinutes || 0);
      setDiscordWebhookUrl(data.discordWebhookUrl || "");
    }
  }, [data]);

  const save = useMutation({
    mutationFn: () =>
      api.saveSettings({
        username,
        password,
        apiKey,
        downloader,
        steamCmdPath,
        depotDownloaderPath,
        maxConcurrentDownloads,
        updateCheckIntervalMinutes,
        discordWebhookUrl,
      }),
    onSuccess: () => {
      toast.success("Settings saved");
      setPassword("");
      queryClient.invalidateQueries({ queryKey: ["settings"] });
    },
    onError: (e) =>
      toast.error("Failed to save", e instanceof ApiError ? e.message : undefined),
  });

  const isGoSteam = downloader === "gosteam";

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
      className="flex flex-col gap-6"
    >
      <PageHeader
        title="Settings"
        description="Steam credentials drive downloads; the API key powers update checks."
      />

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="text-base">Downloader</CardTitle>
          <CardDescription>
            Used for Workshop mods and dedicated server branch installs. All need a
            Steam account that owns Arma 3 (and the dedicated server for branches).
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="downloader">Download tool</Label>
            <Select
              id="downloader"
              value={downloader}
              onChange={(e) => setDownloader(e.target.value as Downloader)}
            >
              <option value="gosteam">Built-in (gosteam) — recommended</option>
              <option value="depotdownloader">DepotDownloader</option>
              <option value="steamcmd">SteamCMD</option>
            </Select>
          </div>

          {isGoSteam ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="maxConcurrent">Concurrent downloads</Label>
              <Input
                id="maxConcurrent"
                type="number"
                min={1}
                max={16}
                value={maxConcurrentDownloads}
                onChange={(e) =>
                  setMaxConcurrentDownloads(Math.max(1, Number(e.target.value) || 1))
                }
              />
              <p className="text-xs text-muted-foreground">
                How many mods download at the same time. Built-in downloader logs in
                once and delta-patches existing mods and server installs, so
                re-downloads are fast. Log in below — no SteamCMD required.
              </p>
            </div>
          ) : downloader === "steamcmd" ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="steamCmdPath">SteamCMD path</Label>
              <Input
                id="steamCmdPath"
                value={steamCmdPath}
                onChange={(e) => setSteamCmdPath(e.target.value)}
                placeholder="e.g. /usr/games/steamcmd (blank = use PATH)"
                autoComplete="off"
              />
              <p className="text-xs text-muted-foreground">
                Used for Workshop mods and dedicated server branch installs. Progress
                is coarse (no per-file %). Leave blank to use the executable on your
                PATH.
              </p>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              <Label htmlFor="depotDownloaderPath">DepotDownloader path</Label>
              <Input
                id="depotDownloaderPath"
                value={depotDownloaderPath}
                onChange={(e) => setDepotDownloaderPath(e.target.value)}
                placeholder="e.g. /opt/DepotDownloader/DepotDownloader (blank = use PATH)"
                autoComplete="off"
              />
              <p className="text-xs text-muted-foreground">
                Used for Workshop mods. Dedicated server branch installs still use
                SteamCMD when this tool is selected (SteamCMD must be on PATH). Leave
                blank to use DepotDownloader on PATH.
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      {isGoSteam && <SteamLogin defaultUsername={username} />}

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="text-base">Steam configuration</CardTitle>
          <CardDescription>
            {isGoSteam
              ? "The built-in downloader uses the login above; no password is stored. The API key powers update checks."
              : "Stored locally in the app database."}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="username">Steam username</Label>
            <Input
              id="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="off"
            />
          </div>
          {!isGoSteam && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="password">Steam password</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={data?.hasPassword ? "•••••••• (leave blank to keep)" : ""}
                autoComplete="new-password"
              />
            </div>
          )}
          <div className="flex flex-col gap-2">
            <Label htmlFor="apiKey">Steam Web API key</Label>
            <Input
              id="apiKey"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              autoComplete="off"
            />
            <p className="text-xs text-muted-foreground">
              Required for Workshop update checks. Get a key at{" "}
              <a
                href="https://steamcommunity.com/dev/apikey"
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-2 hover:text-foreground"
              >
                steamcommunity.com/dev/apikey
              </a>
              .
            </p>
          </div>
        </CardContent>
      </Card>

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="text-base">Update notifications</CardTitle>
          <CardDescription>
            Periodically check Workshop mods and dedicated-server branches. Discord
            alerts fire only when an update affects a server you have configured.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="updateInterval">Auto-check interval</Label>
            <Select
              id="updateInterval"
              value={String(updateCheckIntervalMinutes)}
              onChange={(e) => setUpdateCheckIntervalMinutes(Number(e.target.value))}
            >
              <option value="0">Off</option>
              <option value="15">Every 15 minutes</option>
              <option value="30">Every 30 minutes</option>
              <option value="60">Every hour</option>
              <option value="360">Every 6 hours</option>
              <option value="720">Every 12 hours</option>
              <option value="1440">Every 24 hours</option>
            </Select>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="discordWebhook">Discord webhook URL</Label>
            <Input
              id="discordWebhook"
              value={discordWebhookUrl}
              onChange={(e) => setDiscordWebhookUrl(e.target.value)}
              placeholder="https://discord.com/api/webhooks/…"
              autoComplete="off"
            />
            <p className="text-xs text-muted-foreground">
              Leave blank to update flags in the UI without sending Discord messages.
            </p>
          </div>
        </CardContent>
      </Card>

      <div>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending && <Loader2 className="size-4 animate-spin" />}
          Save settings
        </Button>
      </div>
    </form>
  );
}
