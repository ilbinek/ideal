import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Session, SessionPermissions } from "@/lib/types";

const emptyPermissions: SessionPermissions = {
  isFullAdmin: false,
  canManageMods: false,
  canManageBranches: false,
  serverIds: [],
  canAccessServers: false,
  hasAnyAccess: false,
};

export function useSession() {
  return useQuery({
    queryKey: ["session"],
    queryFn: () => api.session(),
  });
}

export function permissionsOf(session: Session | undefined): SessionPermissions {
  return session?.permissions ?? emptyPermissions;
}
