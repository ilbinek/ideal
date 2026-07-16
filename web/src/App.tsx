import { useEffect } from "react";
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
  useNavigate,
} from "react-router-dom";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, setUnauthorizedHandler } from "@/lib/api";
import { ToastProvider } from "@/components/ui/toast";
import { Layout } from "@/components/Layout";
import { Login } from "@/pages/Login";
import { Dashboard } from "@/pages/Dashboard";
import { Modsets } from "@/pages/Modsets";
import { Mods } from "@/pages/Mods";
import { ModDetail } from "@/pages/ModDetail";
import { SettingsPage } from "@/pages/Settings";
import { Servers } from "@/pages/Servers";
import { ServerCreate } from "@/pages/ServerCreate";
import { ServerDetail } from "@/pages/ServerDetail";
import { Branches } from "@/pages/Branches";
import { StartupCommand } from "@/pages/StartupCommand";
import { UsersPage } from "@/pages/Users";
import { permissionsOf } from "@/hooks/useSession";
import type { SessionPermissions } from "@/lib/types";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

function FullScreenSpinner() {
  return (
    <div className="flex min-h-screen items-center justify-center">
      <Loader2 className="size-6 animate-spin text-muted-foreground" />
    </div>
  );
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  useEffect(() => {
    setUnauthorizedHandler(() => navigate("/login"));
  }, [navigate]);

  const { data, isLoading, isFetched } = useQuery({
    queryKey: ["session"],
    queryFn: () => api.session(),
  });

  if (isLoading || !isFetched) return <FullScreenSpinner />;
  if (!data?.authenticated) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

function RequirePerm({
  allow,
  children,
}: {
  allow: (p: SessionPermissions) => boolean;
  children: React.ReactNode;
}) {
  const { data } = useQuery({
    queryKey: ["session"],
    queryFn: () => api.session(),
  });
  const perms = permissionsOf(data);
  if (!allow(perms)) return <Navigate to="/" replace />;
  return <>{children}</>;
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route
              element={
                <RequireAuth>
                  <Layout />
                </RequireAuth>
              }
            >
              <Route path="/" element={<Dashboard />} />
              <Route
                path="/modsets"
                element={
                  <RequirePerm allow={(p) => p.canManageMods}>
                    <Modsets />
                  </RequirePerm>
                }
              />
              <Route
                path="/mods"
                element={
                  <RequirePerm allow={(p) => p.canManageMods}>
                    <Mods />
                  </RequirePerm>
                }
              />
              <Route
                path="/mods/:id"
                element={
                  <RequirePerm allow={(p) => p.canManageMods}>
                    <ModDetail />
                  </RequirePerm>
                }
              />
              <Route
                path="/servers"
                element={
                  <RequirePerm allow={(p) => p.canAccessServers}>
                    <Servers />
                  </RequirePerm>
                }
              />
              <Route
                path="/servers/new"
                element={
                  <RequirePerm allow={(p) => p.isFullAdmin}>
                    <ServerCreate />
                  </RequirePerm>
                }
              />
              <Route
                path="/servers/:id"
                element={
                  <RequirePerm allow={(p) => p.canAccessServers}>
                    <ServerDetail />
                  </RequirePerm>
                }
              />
              <Route
                path="/branches"
                element={
                  <RequirePerm allow={(p) => p.canManageBranches}>
                    <Branches />
                  </RequirePerm>
                }
              />
              <Route
                path="/settings"
                element={
                  <RequirePerm allow={(p) => p.isFullAdmin}>
                    <SettingsPage />
                  </RequirePerm>
                }
              />
              <Route
                path="/users"
                element={
                  <RequirePerm allow={(p) => p.isFullAdmin}>
                    <UsersPage />
                  </RequirePerm>
                }
              />
              <Route
                path="/startup-command"
                element={
                  <RequirePerm allow={(p) => p.canManageMods}>
                    <StartupCommand />
                  </RequirePerm>
                }
              />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}
