import type { QueryClient } from "@tanstack/react-query"
import {
  Link,
  Outlet,
  RouterProvider,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
  useRouter,
  type ErrorComponentProps,
  type ParsedLocation,
} from "@tanstack/react-router"

import { EmptyState, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { AccountPage } from "@/features/auth/account-page"
import { InvitePage } from "@/features/auth/invite-page"
import { LoginPage } from "@/features/auth/login-page"
import { safeNext, validateLoginSearch } from "@/features/auth/search"
import { JobsPage } from "@/features/jobs/jobs-page"
import { DocumentPage } from "@/features/knowledge/document-page"
import { KnowledgePage } from "@/features/knowledge/knowledge-page"
import { validateDocumentSearch } from "@/features/knowledge/search"
import { validateJobsSearch } from "@/features/jobs/search"
import { MembersPage } from "@/features/members/members-page"
import { PipelinesPage } from "@/features/pipelines/list-page"
import { EditPipelinePage, NewPipelinePage } from "@/features/pipelines/pipeline-pages"
import { validateYouTubeReturn } from "@/features/publishing/youtube-return"
import { validateSeoSearch } from "@/features/seo/search"
import { SeoPage } from "@/features/seo/seo-page"
import { StoriesPage } from "@/features/stories/list-page"
import { NewRequestPage } from "@/features/stories/request-page"
import { validateStoriesSearch } from "@/features/stories/search"
import { StoryPage } from "@/features/stories/story-page"
import { TemplatePage } from "@/features/templates/detail-page"
import { TemplatesPage } from "@/features/templates/list-page"
import { validateTemplatesSearch } from "@/features/templates/search"
import { EditTemplatePage, NewTemplatePage } from "@/features/templates/template-pages"
import { SystemPage } from "@/features/system/system-page"
import { HomePage } from "@/features/workspaces/home-page"
import { WorkspaceSettingsPage } from "@/features/workspaces/settings-page"
import { WorkspaceLayout } from "@/features/workspaces/workspace-layout"
import { m } from "@/i18n"
import { currentUser } from "@/queries/auth"

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

/** A route guard that failed for a reason other than being signed out. */
function RouteError({ error }: ErrorComponentProps) {
  const router = useRouter()
  return (
    <main className="mx-auto max-w-xl p-8">
      <ErrorState error={error} onRetry={() => void router.invalidate()} />
    </main>
  )
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
  errorComponent: RouteError,
})

/** beforeLoad for every page that needs a session: no user, sign in first. */
async function requireUser({
  context,
  location,
}: {
  context: RouterContext
  location: ParsedLocation
}) {
  const me = await currentUser(context.queryClient)
  if (!me) throw redirect({ to: "/login", search: { next: location.href } })
}

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: validateLoginSearch,
  // Asks the server rather than the cache: a signed-in visitor moves on.
  beforeLoad: async ({ context, search }) => {
    const me = await currentUser(context.queryClient, true)
    if (me) throw redirect({ href: safeNext(search.next), replace: true })
  },
  component: LoginPage,
})

const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/invite/$token",
  component: InvitePage,
})

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  // The YouTube consent flow lands here when it lost track of the workspace.
  validateSearch: validateYouTubeReturn,
  beforeLoad: requireUser,
  component: HomePage,
})

const accountRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/account",
  beforeLoad: requireUser,
  component: AccountPage,
})

const systemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/system",
  beforeLoad: requireUser,
  component: SystemPage,
})

const workspaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/w/$workspaceId",
  beforeLoad: requireUser,
  component: WorkspaceLayout,
})

const workspaceIndexRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/w/$workspaceId/stories", params, replace: true })
  },
})

const storiesRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/stories",
  validateSearch: validateStoriesSearch,
  component: StoriesPage,
})

const newStoriesRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/stories/new",
  component: NewRequestPage,
})

const storyRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/stories/$storyId",
  component: StoryPage,
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

const knowledgeRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/knowledge",
  component: KnowledgePage,
})

const documentRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/knowledge/$documentId",
  validateSearch: validateDocumentSearch,
  component: DocumentPage,
})

const seoRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/seo",
  validateSearch: validateSeoSearch,
  component: SeoPage,
})

const templatesRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/templates",
  validateSearch: validateTemplatesSearch,
  component: TemplatesPage,
})

const newTemplateRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/templates/new",
  component: NewTemplatePage,
})

const templateRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/templates/$templateId",
  component: TemplatePage,
})

const editTemplateRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/templates/$templateId/edit",
  component: EditTemplatePage,
})

const jobsRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/jobs",
  validateSearch: validateJobsSearch,
  component: JobsPage,
})

const membersRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/members",
  component: MembersPage,
})

const settingsRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "/settings",
  // Google's consent screen comes back here with ?youtube=connected|error.
  validateSearch: validateYouTubeReturn,
  component: WorkspaceSettingsPage,
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  inviteRoute,
  homeRoute,
  accountRoute,
  systemRoute,
  workspaceRoute.addChildren([
    workspaceIndexRoute,
    storiesRoute,
    newStoriesRoute,
    storyRoute,
    pipelinesRoute,
    newPipelineRoute,
    editPipelineRoute,
    knowledgeRoute,
    documentRoute,
    seoRoute,
    templatesRoute,
    newTemplateRoute,
    templateRoute,
    editTemplateRoute,
    jobsRoute,
    membersRoute,
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
