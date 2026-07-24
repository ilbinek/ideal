import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, Trash2, Users as UsersIcon } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import type { AppUser } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/PageHeader";

export function UsersPage() {
  const queryClient = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.getUsers() });
  const servers = useQuery({ queryKey: ["servers"], queryFn: () => api.getServers() });
  const [error, setError] = useState("");

  const save = useMutation({
    mutationFn: (u: AppUser) =>
      api.updateUser(u.id, {
        isFullAdmin: u.isFullAdmin,
        canManageMods: u.canManageMods,
        canManageBranches: u.canManageBranches,
        serverIds: u.serverIds,
      }),
    onSuccess: () => {
      setError("");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : "Failed to save user");
    },
  });

  const remove = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      setError("");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : "Failed to delete user");
    },
  });

  const create = useMutation({
    mutationFn: (body: {
      steamId: string;
      isFullAdmin: boolean;
      canManageMods: boolean;
      canManageBranches: boolean;
      serverIds: number[];
    }) => api.createUser(body),
    onSuccess: () => {
      setError("");
      queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : "Failed to add user");
    },
  });

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Users"
        description="Add Steam users manually, then grant roles. Only listed users can sign in with Steam."
      />

      {error && (
        <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

      <AddUserCard
        servers={servers.data ?? []}
        creating={create.isPending}
        onCreate={async (body) => {
          await create.mutateAsync(body);
        }}
      />

      {users.isLoading ? (
        <div className="flex justify-center py-12">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      ) : !users.data?.length ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <UsersIcon className="size-4" />
              No Steam users yet
            </CardTitle>
            <CardDescription>
              Add a user above with their SteamID64 or Steam profile URL before they can sign in.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : (
        <div className="flex flex-col gap-4">
          {users.data.map((user) => (
            <UserCard
              key={user.id}
              user={user}
              servers={servers.data ?? []}
              saving={save.isPending}
              onSave={(u) => save.mutate(u)}
              onDelete={() => {
                if (confirm(`Remove ${user.displayName || user.steamId}?`)) {
                  remove.mutate(user.id);
                }
              }}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function AddUserCard({
  servers,
  creating,
  onCreate,
}: {
  servers: { id: number; name: string }[];
  creating: boolean;
  onCreate: (body: {
    steamId: string;
    isFullAdmin: boolean;
    canManageMods: boolean;
    canManageBranches: boolean;
    serverIds: number[];
  }) => Promise<void>;
}) {
  const [steamId, setSteamId] = useState("");
  const [isFullAdmin, setIsFullAdmin] = useState(false);
  const [canManageMods, setCanManageMods] = useState(false);
  const [canManageBranches, setCanManageBranches] = useState(false);
  const [serverIds, setServerIds] = useState<number[]>([]);
  const [localError, setLocalError] = useState("");

  const toggleServer = (serverId: number) => {
    setServerIds((ids) =>
      ids.includes(serverId)
        ? ids.filter((id) => id !== serverId)
        : [...ids, serverId].sort((a, b) => a - b),
    );
  };

  const submit = async () => {
    const id = steamId.trim();
    if (!id) {
      setLocalError("Enter a SteamID64 or Steam profile URL");
      return;
    }
    setLocalError("");
    try {
      await onCreate({
        steamId: id,
        isFullAdmin,
        canManageMods: isFullAdmin || canManageMods,
        canManageBranches: isFullAdmin || canManageBranches,
        serverIds: isFullAdmin ? [] : serverIds,
      });
      setSteamId("");
      setIsFullAdmin(false);
      setCanManageMods(false);
      setCanManageBranches(false);
      setServerIds([]);
    } catch {
      // Parent surfaces API errors.
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Plus className="size-4" />
          Add user
        </CardTitle>
        <CardDescription>
          Paste a SteamID64 or steamcommunity.com profile/vanity URL. Persona name/avatar are
          fetched when a Steam Web API key is configured (required for vanity URLs).
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <Label htmlFor="new-steam-id">Steam ID or profile URL</Label>
          <Input
            id="new-steam-id"
            value={steamId}
            onChange={(e) => setSteamId(e.target.value)}
            placeholder="76561198… or https://steamcommunity.com/id/…"
            className="font-mono"
          />
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          <Toggle
            id="add-full"
            label="Full admin"
            checked={isFullAdmin}
            onChange={(v) => {
              setIsFullAdmin(v);
              if (v) {
                setCanManageMods(true);
                setCanManageBranches(true);
              }
            }}
          />
          <Toggle
            id="add-mods"
            label="Manage mods"
            checked={isFullAdmin || canManageMods}
            disabled={isFullAdmin}
            onChange={setCanManageMods}
          />
          <Toggle
            id="add-branches"
            label="Manage branches"
            checked={isFullAdmin || canManageBranches}
            disabled={isFullAdmin}
            onChange={setCanManageBranches}
          />
        </div>

        <div>
          <Label className="mb-2 block">Server admin</Label>
          {servers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No servers to assign.</p>
          ) : (
            <div className="grid gap-2 sm:grid-cols-2">
              {servers.map((s) => (
                <label
                  key={s.id}
                  className="flex cursor-pointer items-center gap-2 rounded-md border border-border px-3 py-2 text-sm"
                >
                  <input
                    type="checkbox"
                    checked={isFullAdmin || serverIds.includes(s.id)}
                    disabled={isFullAdmin}
                    onChange={() => toggleServer(s.id)}
                  />
                  {s.name}
                </label>
              ))}
            </div>
          )}
        </div>

        {localError && (
          <p className="text-sm text-destructive">{localError}</p>
        )}

        <div className="flex justify-end">
          <Button onClick={submit} disabled={creating || !steamId.trim()}>
            {creating && <Loader2 className="size-4 animate-spin" />}
            Add user
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function UserCard({
  user,
  servers,
  saving,
  onSave,
  onDelete,
}: {
  user: AppUser;
  servers: { id: number; name: string }[];
  saving: boolean;
  onSave: (u: AppUser) => void;
  onDelete: () => void;
}) {
  const [draft, setDraft] = useState(user);

  useEffect(() => {
    setDraft(user);
  }, [user]);

  const dirty = useMemo(
    () =>
      draft.isFullAdmin !== user.isFullAdmin ||
      draft.canManageMods !== user.canManageMods ||
      draft.canManageBranches !== user.canManageBranches ||
      !sameIds(draft.serverIds, user.serverIds),
    [draft, user],
  );

  const toggleServer = (serverId: number) => {
    setDraft((d) => {
      const has = d.serverIds.includes(serverId);
      return {
        ...d,
        serverIds: has
          ? d.serverIds.filter((id) => id !== serverId)
          : [...d.serverIds, serverId].sort((a, b) => a - b),
      };
    });
  };

  const neverLoggedIn = !user.lastLoginAt;

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div className="flex items-center gap-3">
          {user.avatarUrl ? (
            <img
              src={user.avatarUrl}
              alt=""
              className="size-10 rounded-md"
            />
          ) : (
            <div className="flex size-10 items-center justify-center rounded-md bg-secondary text-sm font-medium">
              {(user.displayName || "?").slice(0, 1)}
            </div>
          )}
          <div>
            <CardTitle className="text-base">{user.displayName || user.steamId}</CardTitle>
            <CardDescription className="font-mono text-xs">{user.steamId}</CardDescription>
            {neverLoggedIn && (
              <p className="mt-1 text-xs text-muted-foreground">Never logged in</p>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {user.isFullAdmin && <Badge>Full admin</Badge>}
          {neverLoggedIn && <Badge variant="secondary">Pending login</Badge>}
          <Button variant="ghost" size="icon" onClick={onDelete} title="Remove user">
            <Trash2 className="size-4" />
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-3">
          <Toggle
            id={`full-${user.id}`}
            label="Full admin"
            checked={draft.isFullAdmin}
            onChange={(v) =>
              setDraft((d) => ({
                ...d,
                isFullAdmin: v,
                canManageMods: v ? true : d.canManageMods,
                canManageBranches: v ? true : d.canManageBranches,
              }))
            }
          />
          <Toggle
            id={`mods-${user.id}`}
            label="Manage mods"
            checked={draft.isFullAdmin || draft.canManageMods}
            disabled={draft.isFullAdmin}
            onChange={(v) => setDraft((d) => ({ ...d, canManageMods: v }))}
          />
          <Toggle
            id={`branches-${user.id}`}
            label="Manage branches"
            checked={draft.isFullAdmin || draft.canManageBranches}
            disabled={draft.isFullAdmin}
            onChange={(v) => setDraft((d) => ({ ...d, canManageBranches: v }))}
          />
        </div>

        <div>
          <Label className="mb-2 block">Server admin</Label>
          {servers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No servers to assign.</p>
          ) : (
            <div className="grid gap-2 sm:grid-cols-2">
              {servers.map((s) => (
                <label
                  key={s.id}
                  className="flex cursor-pointer items-center gap-2 rounded-md border border-border px-3 py-2 text-sm"
                >
                  <input
                    type="checkbox"
                    checked={draft.isFullAdmin || draft.serverIds.includes(s.id)}
                    disabled={draft.isFullAdmin}
                    onChange={() => toggleServer(s.id)}
                  />
                  {s.name}
                </label>
              ))}
            </div>
          )}
        </div>

        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            disabled={!dirty || saving}
            onClick={() => setDraft(user)}
          >
            Reset
          </Button>
          <Button disabled={!dirty || saving} onClick={() => onSave(draft)}>
            {saving && <Loader2 className="size-4 animate-spin" />}
            Save
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function Toggle({
  id,
  label,
  checked,
  disabled,
  onChange,
}: {
  id: string;
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label
      htmlFor={id}
      className="flex cursor-pointer items-center gap-2 rounded-md border border-border px-3 py-2 text-sm"
    >
      <input
        id={id}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      {label}
    </label>
  );
}

function sameIds(a: number[], b: number[]) {
  if (a.length !== b.length) return false;
  const as = [...a].sort((x, y) => x - y);
  const bs = [...b].sort((x, y) => x - y);
  return as.every((v, i) => v === bs[i]);
}
