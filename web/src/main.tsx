import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query"

import "./index.css"
import { ApiError, isApiError } from "@/api/client"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Toaster } from "@/components/ui/sonner"
import { isPublicPath } from "@/features/auth/search"
import { followSystemTheme } from "@/lib/theme"
import { isUnauthenticated } from "@/queries/auth"
import { keys } from "@/queries/keys"
import { AppRouter, router } from "./router"

followSystemTheme()

/**
 * A 401 anywhere means the session ended (expired, signed out elsewhere,
 * password changed on another device): forget the cached data and go to
 * the sign-in page, which brings the user back here afterwards. Only a
 * session that existed can end — the guards handle "never signed in".
 */
function onApiError(err: unknown) {
  if (!isUnauthenticated(err)) return
  if (queryClient.getQueryData(keys.me()) === undefined) return
  const { pathname, href } = router.state.location
  if (isPublicPath(pathname)) return
  queryClient.removeQueries()
  void router.navigate({ to: "/login", search: { next: href }, replace: true })
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: onApiError }),
  mutationCache: new MutationCache({
    onError: (err, _vars, _ctx, mutation) => {
      if (!mutation.meta?.ownsUnauthenticated) onApiError(err)
      // Refused for the role: our idea of the role may be stale (an owner
      // changed it), so refetch the workspaces that carry it.
      if (isApiError(err, "Forbidden")) {
        void queryClient.invalidateQueries({
          predicate: (q) => q.queryKey[0] === "workspaces" && q.queryKey.length <= 2,
        })
      }
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
      // A 4xx will not get better by asking again.
      retry: (failures, err) =>
        !(err instanceof ApiError && err.status >= 400 && err.status < 500) && failures < 2,
    },
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <AppRouter queryClient={queryClient} />
        <Toaster position="top-right" richColors closeButton />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
)
