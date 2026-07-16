import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Check, Copy } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PageHeader } from "@/components/PageHeader";

export function StartupCommand() {
  const [params] = useSearchParams();
  const modset = Number(params.get("modset"));
  const [copied, setCopied] = useState(false);

  const { data, isLoading, isError } = useQuery({
    queryKey: ["startup-command", modset],
    queryFn: () => api.startupCommand(modset),
    enabled: !!modset,
  });

  const copy = async () => {
    if (!data?.command) return;
    await navigator.clipboard.writeText(data.command);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start gap-3">
        <Button asChild variant="ghost" size="icon" className="mt-0.5 shrink-0">
          <Link to="/modsets">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
        <PageHeader
          className="flex-1"
          title="Startup command"
          description="Server mod parameter and extracted keys for this modset."
        />
      </div>

      {!modset && <p className="text-sm text-destructive">No modset selected.</p>}
      {isLoading && <p className="text-sm text-muted-foreground">Generating…</p>}
      {isError && <p className="text-sm text-destructive">Failed to generate command.</p>}

      {data && (
        <>
          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <div>
                <CardTitle className="text-base">Mods parameter</CardTitle>
                <CardDescription>Paste into your server launch arguments.</CardDescription>
              </div>
              <Button variant="outline" size="sm" onClick={copy}>
                {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
                {copied ? "Copied" : "Copy"}
              </Button>
            </CardHeader>
            <CardContent>
              <pre className="overflow-x-auto rounded-md bg-muted p-4 text-xs">
                <code className="font-mono">{data.command}</code>
              </pre>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Keys</CardTitle>
              <CardDescription>
                BIKEY files were extracted to <code>{data.keysPath}</code>.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {data.modPaths.length > 0 ? (
                <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
                  {data.modPaths.map((p) => (
                    <li key={p} className="font-mono">
                      {p}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-sm text-muted-foreground">
                  No downloaded mods in this modset.
                </p>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
