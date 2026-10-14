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
