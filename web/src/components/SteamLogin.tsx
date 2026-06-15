import * as React from "react";
import { QRCodeSVG } from "qrcode.react";
import { Loader2, CheckCircle2, LogOut } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { SteamLoginState } from "@/lib/types";
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

/**
 * Drives the built-in (gosteam) interactive Steam login. State lives on the
 * server and streams over SSE, so Steam Guard prompts and QR challenges appear
 * live and survive reloads. Once logged in, gosteam caches a refresh token and
 * never needs to relog.
 */
export function SteamLogin({ defaultUsername }: { defaultUsername: string }) {
  const toast = useToast();
  const [state, setState] = React.useState<SteamLoginState>({ phase: "idle" });
  const [username, setUsername] = React.useState(defaultUsername);
  const [password, setPassword] = React.useState("");
  const [code, setCode] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => setUsername(defaultUsername), [defaultUsername]);

  React.useEffect(() => {
    const source = new EventSource("/api/steam/login/stream");
    source.onmessage = (e) => {
      try {
        setState(JSON.parse(e.data) as SteamLoginState);
      } catch {
        /* ignore malformed events */
      }
    };
    return () => source.close();
  }, []);

  const onError = (e: unknown) =>
    toast.error("Steam login", e instanceof ApiError ? e.message : "request failed");

  const startCredentials = async () => {
    setBusy(true);
    try {
      await api.steamLogin({ method: "credentials", username, password });
      setPassword("");
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };

  const startQR = async () => {
    setBusy(true);
    try {
      await api.steamLogin({ method: "qr" });
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };

  const submitGuard = async () => {
    setBusy(true);
    try {
      await api.steamGuard(code.trim());
      setCode("");
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };

  const logout = async () => {
    try {
      await api.steamLogout();
    } catch (e) {
      onError(e);
    }
  };

  const inProgress =
    state.phase === "connecting" ||
    state.phase === "guard_required" ||
    state.phase === "qr_challenge";

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="text-base">Steam login</CardTitle>
        <CardDescription>
          Sign in once with the built-in downloader. Your refresh token is cached
          locally, so you won't need to log in again. No password is stored.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {state.phase === "logged_in" ? (
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-2 text-sm">
              <CheckCircle2 className="size-4 text-green-500" />
              Logged in{state.username ? ` as ${state.username}` : ""}
            </div>
            <Button type="button" variant="outline" onClick={logout}>
              <LogOut className="size-4" />
              Log out
            </Button>
          </div>
        ) : state.phase === "qr_challenge" ? (
          <div className="flex flex-col items-center gap-3">
            <p className="text-sm text-muted-foreground">
              Scan with the Steam mobile app to approve sign-in.
            </p>
            {state.challengeUrl ? (
              <div className="rounded-md bg-white p-3">
                <QRCodeSVG value={state.challengeUrl} size={200} />
              </div>
            ) : (
              <Loader2 className="size-5 animate-spin" />
            )}
          </div>
        ) : state.phase === "guard_required" ? (
          <div className="flex flex-col gap-2">
            <Label htmlFor="guardCode">
              Steam Guard code
              {state.guardKind === "email"
                ? " (sent to your email)"
                : state.guardKind === "device"
                  ? " (from the Steam mobile app)"
                  : ""}
            </Label>
            <div className="flex gap-2">
              <Input
                id="guardCode"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="e.g. ABCDE"
                autoComplete="one-time-code"
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    submitGuard();
                  }
                }}
              />
              <Button type="button" onClick={submitGuard} disabled={busy || !code.trim()}>
                {busy && <Loader2 className="size-4 animate-spin" />}
                Submit
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="flex flex-col gap-2">
              <Label htmlFor="steamUser">Steam username</Label>
              <Input
                id="steamUser"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="off"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="steamPass">Steam password</Label>
              <Input
                id="steamPass"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
              />
            </div>
            <div className="flex gap-2">
              <Button
                type="button"
                onClick={startCredentials}
                disabled={busy || inProgress || !username || !password}
              >
                {(busy || state.phase === "connecting") && (
                  <Loader2 className="size-4 animate-spin" />
                )}
                Log in
              </Button>
              <Button type="button" variant="outline" onClick={startQR} disabled={busy || inProgress}>
                Use QR code instead
              </Button>
            </div>
          </>
        )}

        {state.phase === "error" && state.error && (
          <p className="text-sm text-destructive">{state.error}</p>
        )}
        {state.phase === "connecting" && (
          <p className="text-sm text-muted-foreground">Connecting to Steam…</p>
        )}
      </CardContent>
    </Card>
  );
}
