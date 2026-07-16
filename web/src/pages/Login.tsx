import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Package } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { CookieNotice } from "@/components/CookieNotice";

export function Login() {
  const [password, setPassword] = useState("");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const steamError = searchParams.get("error");

  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api.session(),
  });

  useEffect(() => {
    if (session.data?.authenticated) {
      navigate("/", { replace: true });
    }
  }, [session.data?.authenticated, navigate]);

  const login = useMutation({
    mutationFn: () => api.login(password),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/", { replace: true });
    },
  });

  const steamEnabled = session.data?.steamLoginEnabled === true;

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <div className="mb-2 flex size-12 items-center justify-center rounded-xl bg-primary/15">
            <Package className="size-6 text-primary" />
          </div>
          <CardTitle>IDEAL</CardTitle>
          <CardDescription>Arma 3 Mod Manager</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              login.mutate();
            }}
            className="flex flex-col gap-4"
          >
            <div className="flex flex-col gap-2">
              <Label htmlFor="password">Admin password</Label>
              <Input
                id="password"
                type="password"
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter password"
              />
            </div>
            {login.isError && (
              <p className="text-sm text-destructive">
                {login.error instanceof ApiError
                  ? login.error.message
                  : "Login failed"}
              </p>
            )}
            <Button type="submit" disabled={login.isPending || !password}>
              {login.isPending && <Loader2 className="size-4 animate-spin" />}
              Sign in
            </Button>
          </form>

          {steamEnabled && (
            <>
              <div className="relative flex items-center gap-3">
                <div className="h-px flex-1 bg-border" />
                <span className="text-xs text-muted-foreground">or</span>
                <div className="h-px flex-1 bg-border" />
              </div>
              {steamError && (
                <p className="text-sm text-destructive">
                  {steamError === "steam_cancelled"
                    ? "Steam sign-in was cancelled."
                    : "Steam sign-in failed. Try again."}
                </p>
              )}
              <Button
                type="button"
                variant="outline"
                className="w-full border-[#1b2838] bg-[#171a21] text-white hover:bg-[#1b2838] hover:text-white"
                asChild
              >
                <a href="/api/auth/steam">
                  <SteamIcon className="size-4" />
                  Sign in through Steam
                </a>
              </Button>
            </>
          )}
        </CardContent>
      </Card>
      <CookieNotice />
    </div>
  );
}

function SteamIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor" aria-hidden>
      <path d="M12 2a10 10 0 0 0-10 9.37l5.4 2.23a2.7 2.7 0 0 1 1.52-.46c.14 0 .27.01.4.03l2.4-3.47v-.05a3.43 3.43 0 1 1 3.43 3.43h-.07l-3.42 2.44c0 .11.02.22.02.34a2.71 2.71 0 0 1-2.71 2.71 2.72 2.72 0 0 1-2.66-2.18L2.3 14.4A10 10 0 1 0 12 2zm-3.84 14.37-2.18-.9a2.72 2.72 0 0 0 5.2.76l.02-.1-2.45-1.01a1.9 1.9 0 0 1-.59.25zm9.7-7.2a2.4 2.4 0 1 0-2.4 2.4 2.4 2.4 0 0 0 2.4-2.4zm-4.14 0a1.74 1.74 0 1 1 1.74 1.74 1.74 1.74 0 0 1-1.74-1.74z" />
    </svg>
  );
}
