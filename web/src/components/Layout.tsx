import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Boxes, Download, Home, LogOut, Package, Server, Settings } from "lucide-react";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { DownloadsProvider, useDownloads } from "@/hooks/useDownloads";
import { ServersProvider } from "@/hooks/useServers";

const navItems = [
  { to: "/", label: "Dashboard", icon: Home, end: true },
  { to: "/servers", label: "Servers", icon: Server },
  { to: "/modsets", label: "Modsets", icon: Boxes },
  { to: "/mods", label: "Mods", icon: Package },
];

function QueueIndicator() {
  const { snapshot } = useDownloads();
  if (!snapshot.processing && snapshot.total === 0) return null;
  return (
    <div className="flex items-center gap-2 rounded-md bg-secondary px-3 py-1.5 text-xs text-secondary-foreground">
      <Download
        className={cn("size-3.5", snapshot.processing && "animate-pulse text-primary")}
      />
      <span>
        {snapshot.processing
          ? `Downloading ${snapshot.done}/${snapshot.total}`
          : `Queue: ${snapshot.done}/${snapshot.total} done`}
      </span>
    </div>
  );
}

function Shell() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const logout = useMutation({
    mutationFn: () => api.logout(),
    onSettled: () => {
      queryClient.clear();
      navigate("/login");
    },
  });

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-border bg-card/80 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4">
          <div className="flex items-center gap-2 font-semibold">
            <Package className="size-5 text-primary" />
            <span>IDEAL</span>
            <span className="hidden text-xs font-normal text-muted-foreground sm:inline">
              Arma 3 Server Manager
            </span>
          </div>
          <nav className="flex items-center gap-1">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                    isActive
                      ? "bg-secondary text-foreground"
                      : "text-muted-foreground hover:bg-accent hover:text-foreground",
                  )
                }
              >
                <item.icon className="size-4" />
                <span className="hidden sm:inline">{item.label}</span>
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <QueueIndicator />
            <NavLink
              to="/settings"
              className={({ isActive }) =>
                cn(
                  "rounded-md p-2 transition-colors hover:bg-accent hover:text-foreground",
                  isActive ? "text-foreground" : "text-muted-foreground",
                )
              }
              title="Settings"
            >
              <Settings className="size-4" />
            </NavLink>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => logout.mutate()}
              title="Log out"
            >
              <LogOut className="size-4" />
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-8">
        <Outlet />
      </main>
    </div>
  );
}

export function Layout() {
  return (
    <DownloadsProvider>
      <ServersProvider>
        <Shell />
      </ServersProvider>
    </DownloadsProvider>
  );
}
