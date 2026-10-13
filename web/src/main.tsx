import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

import "./index.css"
import { ApiError } from "@/api/client"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Toaster } from "@/components/ui/sonner"
import { followSystemTheme } from "@/lib/theme"
import { AppRouter } from "./router"

followSystemTheme()

const queryClient = new QueryClient({
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
