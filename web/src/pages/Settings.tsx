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

export function SettingsPage() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: () => api.getSettings() });

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [downloader, setDownloader] = useState<Downloader>("depotdownloader");
  const [steamCmdPath, setSteamCmdPath] = useState("");
  const [depotDownloaderPath, setDepotDownloaderPath] = useState("");

  useEffect(() => {
    if (data) {
      setUsername(data.username);
      setApiKey(data.apiKey);
      setDownloader(data.downloader);
      setSteamCmdPath(data.steamCmdPath);
      setDepotDownloaderPath(data.depotDownloaderPath);
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
      }),
    onSuccess: () => {
      toast.success("Settings saved");
      setPassword("");
      queryClient.invalidateQueries({ queryKey: ["settings"] });
    },
    onError: (e) =>
      toast.error("Failed to save", e instanceof ApiError ? e.message : undefined),
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
      className="flex flex-col gap-6"
    >
      <div>
        <h1 className="text-2xl font-semibold">Settings</h1>
        <p className="text-sm text-muted-foreground">
          Steam credentials drive downloads; the API key powers update checks.
        </p>
      </div>

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="text-base">Downloader</CardTitle>
          <CardDescription>
            Choose which tool fetches Workshop mods. Both need a Steam account that
            owns Arma 3.
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
              <option value="depotdownloader">DepotDownloader</option>
              <option value="steamcmd">SteamCMD</option>
            </Select>
          </div>

          {downloader === "steamcmd" ? (
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
                Progress is coarse with SteamCMD (no per-file %). Leave blank to use
                the executable on your PATH.
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
                Leave blank to use the executable on your PATH.
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="text-base">Steam configuration</CardTitle>
          <CardDescription>Stored locally in the app database.</CardDescription>
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
          <div className="flex flex-col gap-2">
            <Label htmlFor="apiKey">Steam Web API key</Label>
            <Input
              id="apiKey"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              autoComplete="off"
            />
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
