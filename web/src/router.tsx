import type { QueryClient } from "@tanstack/react-query"
import {
  Link,
  Outlet,
  RouterProvider,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from "@tanstack/react-router"

import { EmptyState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { JobsPage } from "@/features/jobs/jobs-page"
import { validateJobsSearch } from "@/features/jobs/search"
import { PipelinesPage } from "@/features/pipelines/list-page"
import { EditPipelinePage, NewPipelinePage } from "@/features/pipelines/pipeline-pages"
import { HomePage } from "@/features/workspaces/home-page"
import { WorkspaceSettingsPage } from "@/features/workspaces/settings-page"
import { WorkspaceLayout } from "@/features/workspaces/workspace-layout"
import { m } from "@/i18n"

export type RouterContext = { queryClient: QueryClient }

function NotFound() {
  return (
    <main className="mx-auto max-w-xl p-8">
      <EmptyState
        title={m.errors.notFoundTitle}
        body={m.errors.notFoundBody}
        action={
          <Button render={<Link to="/" />} nativeButton={false}>
            {m.errors.goHome}
          </Button>
        }
      />
    </main>
  )
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
})

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomePage,
})

const workspaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/w/$workspaceId",
  component: WorkspaceLayout,
})

const workspaceIndexRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/w/$workspaceId/pipelines", params, replace: true })
  },
})

const pipelinesRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/pipelines",
  component: PipelinesPage,
})

const newPipelineRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/pipelines/new",
  component: NewPipelinePage,
})

const editPipelineRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/pipelines/$pipelineId",
  component: EditPipelinePage,
})

const jobsRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/jobs",
  validateSearch: validateJobsSearch,
  component: JobsPage,
})

const settingsRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/settings",
  component: WorkspaceSettingsPage,
})

const routeTree = rootRoute.addChildren([
  homeRoute,
  workspaceRoute.addChildren([
    workspaceIndexRoute,
    pipelinesRoute,
    newPipelineRoute,
    editPipelineRoute,
    jobsRoute,
    settingsRoute,
  ]),
])

export const router = createRouter({
  routeTree,
  // Filled in by <AppRouter>; the router itself is a module singleton so
  // its type can be registered below.
  context: { queryClient: undefined! },
  defaultPreload: "intent",
  scrollRestoration: true,
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

export function AppRouter({ queryClient }: { queryClient: QueryClient }) {
  return <RouterProvider router={router} context={{ queryClient }} />
}
