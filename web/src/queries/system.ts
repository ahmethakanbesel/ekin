import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type HostProfile } from "@/api/client"
import { keys } from "./keys"

export const systemQuery = () =>
  queryOptions({
    queryKey: keys.system(),
    queryFn: ({ signal }) => call(api.GET("/system", { signal })),
  })

/** Host profiles and the one this server's worker uses. Admins only. */
export const profilesQuery = () =>
  queryOptions({
    queryKey: keys.profiles(),
    queryFn: ({ signal }) => call(api.GET("/system/profiles", { signal })),
  })

/** API keys and service settings; secrets come back only as a hint. Admins only. */
export const credentialsQuery = () =>
  queryOptions({
    queryKey: keys.credentials(),
    queryFn: async ({ signal }) => {
      const res = await call(api.GET("/system/credentials", { signal }))
      return res.credentials
    },
  })

/** Sets credentials by key; "" removes the saved value so the environment applies again. */
export function useUpdateCredentials() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (values: Record<string, string>) => {
      const res = await call(api.PATCH("/system/credentials", { body: { values } }))
      return res.credentials
    },
    onSuccess: (credentials) => {
      qc.setQueryData(keys.credentials(), credentials)
      // The checks reflect the new keys (Pexels, SMTP, YouTube…).
      void qc.invalidateQueries({ queryKey: keys.system(), exact: true })
      // Every workspace's OpenSEO connection follows the instance's key.
      void qc.invalidateQueries({
        predicate: (q) => q.queryKey[0] === "workspaces" && q.queryKey[3] === "openseo",
      })
    },
  })
}

export function useSaveProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (profile: HostProfile) =>
      call(
        api.PUT("/system/profiles/{name}", {
          params: { path: { name: profile.name } },
          body: profile,
        })
      ),
    onSuccess: (saved) => {
      qc.setQueryData<{ profiles: HostProfile[]; current: string }>(keys.profiles(), (old) => {
        if (!old) return old
        const exists = old.profiles.some((p) => p.name === saved.name)
        return {
          ...old,
          profiles: exists
            ? old.profiles.map((p) => (p.name === saved.name ? saved : p))
            : [...old.profiles, saved],
        }
      })
      void qc.invalidateQueries({ queryKey: keys.profiles() })
    },
  })
}
