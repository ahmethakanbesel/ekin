import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query"

import { api, call, isApiError, type Schemas, type User } from "@/api/client"
import { keys } from "./keys"

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: {
      /** A 401 from this mutation means a wrong password, not an ended
       * session, so the global handler must leave it alone. */
      ownsUnauthenticated?: boolean
    }
  }
}

export const isUnauthenticated = (err: unknown) => isApiError(err) && err.status === 401

/** The signed-in user. Fails with 401 when there is no session. */
export const meQuery = () =>
  queryOptions({
    queryKey: keys.me(),
    queryFn: ({ signal }) => call(api.GET("/me", { signal })),
  })

/**
 * The signed-in user, or null without a session. `fresh` asks the server
 * instead of trusting the cache.
 */
export async function currentUser(qc: QueryClient, fresh = false): Promise<User | null> {
  try {
    return fresh
      ? await qc.fetchQuery({ ...meQuery(), staleTime: 0 })
      : await qc.ensureQueryData(meQuery())
  } catch (err) {
    if (isUnauthenticated(err)) return null
    throw err
  }
}

/** Forgets everything cached for the previous user and remembers the new one. */
function startSession(qc: QueryClient, user: User) {
  qc.removeQueries()
  qc.setQueryData(keys.me(), user)
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas["LoginInput"]) => call(api.POST("/auth/login", { body })),
    meta: { ownsUnauthenticated: true },
    onSuccess: (user) => startSession(qc, user),
  })
}

export function useLogout() {
  return useMutation({
    mutationFn: async () => {
      try {
        await call(api.POST("/auth/logout"))
      } catch (err) {
        // Already signed out is as good as signing out.
        if (!isUnauthenticated(err)) throw err
      }
    },
    meta: { ownsUnauthenticated: true },
  })
}

export function useUpdateMe() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { name?: string; emailNotifications?: boolean }) =>
      call(api.PATCH("/me", { body })),
    onSuccess: (user, body) => {
      qc.setQueryData(keys.me(), user)
      if (body.name === undefined) return
      // Member lists show the name.
      void qc.invalidateQueries({
        predicate: (q) => q.queryKey[0] === "workspaces" && q.queryKey[2] === "members",
      })
    },
  })
}

export function useChangePassword() {
  return useMutation({
    // A wrong current password is 400 ValidationFailed; a 401 here really
    // is an ended session and goes through the global handler.
    mutationFn: (body: { currentPassword: string; newPassword: string }) =>
      call(api.POST("/me/password", { body })),
  })
}

export const invitationQuery = (token: string) =>
  queryOptions({
    queryKey: keys.invitation(token),
    queryFn: ({ signal }) =>
      call(api.GET("/invitations/{token}", { params: { path: { token } }, signal })),
    // A preview does not change under the reader; refetching would only
    // flip an accepted invitation to "not found" while leaving the page.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  })

export function useAcceptInvitation(token: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { name?: string; password: string }) =>
      call(api.POST("/invitations/{token}/accept", { params: { path: { token } }, body })),
    meta: { ownsUnauthenticated: true },
    onSuccess: ({ user }) => startSession(qc, user),
  })
}
