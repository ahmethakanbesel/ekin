import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query"

import { api, call, type Invitation, type Member, type Role, type Workspace } from "@/api/client"
import { keys } from "./keys"

export const membersQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.members(workspaceId),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/members", {
          params: { path: { workspaceId } },
          signal,
        }),
      )
      return res.members
    },
  })

export const invitationsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.invitations(workspaceId),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/invitations", {
          params: { path: { workspaceId } },
          signal,
        }),
      )
      return res.invitations
    },
  })

/**
 * Changing your own role changes what the workspace lets you do, so the
 * workspace (which carries `role`) is refetched too.
 */
export function useChangeMemberRole(workspaceId: string, selfId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: Role }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/members/{userId}", {
          params: { path: { workspaceId, userId } },
          body: { role },
        }),
      ),
    onSuccess: (member) => {
      qc.setQueryData<Member[]>(keys.members(workspaceId), (old) =>
        old?.map((x) => (x.userId === member.userId ? member : x)),
      )
      void qc.invalidateQueries({ queryKey: keys.members(workspaceId), exact: true })
      if (member.userId === selfId) {
        void qc.invalidateQueries({ queryKey: keys.workspace(workspaceId), exact: true })
        void qc.invalidateQueries({ queryKey: keys.workspaces(), exact: true })
      }
    },
  })
}

/**
 * Removes a member; with your own id, leaves the workspace. After leaving,
 * the caller moves off the workspace and then calls forgetWorkspace —
 * dropping the cache first would refetch the open pages into 404s.
 */
export function useRemoveMember(workspaceId: string, selfId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (userId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/members/{userId}", {
          params: { path: { workspaceId, userId } },
        }),
      )
      return userId
    },
    onSuccess: (userId) => {
      if (userId === selfId) return
      qc.setQueryData<Member[]>(keys.members(workspaceId), (old) =>
        old?.filter((x) => x.userId !== userId),
      )
      void qc.invalidateQueries({ queryKey: keys.members(workspaceId), exact: true })
    },
  })
}

/** Drops a workspace the user no longer belongs to from the cache. */
export function forgetWorkspace(qc: QueryClient, workspaceId: string) {
  qc.removeQueries({ queryKey: keys.workspace(workspaceId) })
  qc.setQueryData<Workspace[]>(keys.workspaces(), (old) => old?.filter((w) => w.id !== workspaceId))
  void qc.invalidateQueries({ queryKey: keys.workspaces(), exact: true })
}

export function useCreateInvitation(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { email: string; role: Role }) =>
      call(
        api.POST("/workspaces/{workspaceId}/invitations", {
          params: { path: { workspaceId } },
          body,
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.invitations(workspaceId) })
    },
  })
}

export function useRevokeInvitation(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (invitationId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/invitations/{invitationId}", {
          params: { path: { workspaceId, invitationId } },
        }),
      )
      return invitationId
    },
    onSuccess: (invitationId) => {
      qc.setQueryData<Invitation[]>(keys.invitations(workspaceId), (old) =>
        old?.filter((i) => i.id !== invitationId),
      )
      void qc.invalidateQueries({ queryKey: keys.invitations(workspaceId) })
    },
  })
}
