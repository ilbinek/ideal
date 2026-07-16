import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Boxes,
  Download,
  Home,
  LogOut,
  Menu,
  Package,
  Server,
  Settings,
  Users,
  X,
} from "lucide-react";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { CookieNotice } from "@/components/CookieNotice";
import { DownloadsProvider, useDownloads } from "@/hooks/useDownloads";
import { ServersProvider } from "@/hooks/useServers";
import { permissionsOf, useSession } from "@/hooks/useSession";

function QueueIndicator() {
  const { snapshot } = useDownloads();
  if (!snapshot.processing && snapshot.total === 0) return null;
  return (
    <div className="flex items-center gap-2 rounded-md border border-border bg-secondary px-3 py-1.5 text-xs text-muted-foreground">
      <Download
        className={cn("size-3.5", snapshot.processing && "animate-pulse text-foreground")}
      />
      <span>
        {snapshot.processing
          ? `Downloading ${snapshot.done}/${snapshot.total}`
          : `Queue: ${snapshot.done}/${snapshot.total} done`}
      </span>
    </div>
  );
}

function BrandMark({ compact }: { compact?: boolean }) {
  return (
    <div className={cn("flex items-center gap-2.5", compact && "gap-2")}>
      <div className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border bg-secondary">
        <Package className="size-4 text-foreground" />
      </div>
      <div className="min-w-0 leading-tight">
        <div className="truncate text-sm font-semibold tracking-wide">IDEAL</div>
        {!compact && (
          <div className="truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            Server Manager
          </div>
        )}
      </div>
    </div>
  );
}

function Shell() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: session } = useSession();
  const perms = permissionsOf(session);
  const [mobileOpen, setMobileOpen] = useState(false);

  const navItems = [
    { to: "/", label: "Dashboard", icon: Home, end: true, show: true },
    {
      to: "/servers",
      label: "Servers",
      icon: Server,
      show: perms.canAccessServers,
    },
    {
      to: "/modsets",
      label: "Modsets",
      icon: Boxes,
      show: perms.canManageMods,
    },
    {
      to: "/mods",
      label: "Mods",
      icon: Package,
      show: perms.canManageMods,
    },
    {
      to: "/users",
      label: "Users",
      icon: Users,
      show: perms.isFullAdmin,
    },
  ].filter((item) => item.show);

  const logout = useMutation({
    mutationFn: () => api.logout(),
    onSettled: () => {
      queryClient.clear();
      navigate("/login");
    },
  });

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    cn(
      "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors",
      isActive
        ? "bg-secondary text-foreground"
        : "text-muted-foreground hover:bg-secondary/70 hover:text-foreground",
    );

  const sidebar = (
    <aside className="flex h-full w-[220px] flex-col border-r border-border bg-card">
      <div className="flex h-14 items-center gap-2 border-b border-border px-4">
        <BrandMark />
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto lg:hidden"
          onClick={() => setMobileOpen(false)}
          title="Close menu"
        >
          <X className="size-4" />
        </Button>
      </div>
      <nav className="flex flex-1 flex-col gap-0.5 p-3">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={navLinkClass}
            onClick={() => setMobileOpen(false)}
          >
            <item.icon className="size-4 shrink-0" />
            {item.label}
          </NavLink>
        ))}
      </nav>
    </aside>
  );

  return (
    <div className="flex min-h-screen bg-background">
      <div className="sticky top-0 hidden h-screen shrink-0 lg:block">{sidebar}</div>

      {mobileOpen && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button
            type="button"
            className="absolute inset-0 bg-black/70"
            aria-label="Close menu"
            onClick={() => setMobileOpen(false)}
          />
          <div className="absolute inset-y-0 left-0 border-r border-border">{sidebar}</div>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-40 flex h-14 items-center gap-3 border-b border-border bg-background px-4">
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={() => setMobileOpen(true)}
            title="Open menu"
          >
            <Menu className="size-4" />
          </Button>
          <div className="lg:hidden">
            <BrandMark compact />
          </div>
          <div className="ml-auto flex items-center gap-2 sm:gap-3">
            {perms.canManageMods && <QueueIndicator />}
            {session?.user?.displayName && (
              <span className="hidden max-w-[160px] truncate text-xs text-muted-foreground md:inline">
                {session.user.displayName}
              </span>
            )}
            {perms.isFullAdmin && (
              <NavLink
                to="/settings"
                className={({ isActive }) =>
                  cn(
                    "rounded-md p-2 transition-colors hover:bg-secondary hover:text-foreground",
                    isActive ? "bg-secondary text-foreground" : "text-muted-foreground",
                  )
                }
                title="Settings"
              >
                <Settings className="size-4" />
              </NavLink>
            )}
            <Button
              variant="ghost"
              size="icon"
              onClick={() => logout.mutate()}
              title="Log out"
            >
              <LogOut className="size-4" />
            </Button>
          </div>
        </header>

        <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6 sm:py-8">
          <Outlet />
        </main>
        <CookieNotice />
      </div>
    </div>
  );
}

export function Layout() {
  const { data: session } = useSession();
  const perms = permissionsOf(session);

  let content = <Shell />;
  if (perms.canAccessServers) {
    content = <ServersProvider>{content}</ServersProvider>;
  }
  if (perms.canManageMods) {
    content = <DownloadsProvider>{content}</DownloadsProvider>;
  }
  return content;
}
