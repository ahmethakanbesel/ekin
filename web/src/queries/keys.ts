import type { DeliverableStatus, JobStatus, LibraryAssetKind, OpportunityStatus } from "@/api/client"

/** A paged list's server-side filter, as its query key holds it. */
type Paged<F> = F & { q?: string; limit: number }

// Hierarchical keys: invalidating ["workspaces", id] also refreshes that
// workspace's pipelines, documents, jobs, members and stories.
export const keys = {
  me: () => ["me"] as const,
  system: () => ["system"] as const,
  profiles: () => ["system", "profiles"] as const,
  credentials: () => ["system", "credentials"] as const,
  voices: () => ["voices"] as const,
  voiceSettings: (workspaceId: string) => ["workspaces", workspaceId, "voice"] as const,
  elevenLabsAccount: (workspaceId: string) => ["workspaces", workspaceId, "voice", "elevenlabs"] as const,
  integrations: (workspaceId: string) => ["workspaces", workspaceId, "integrations"] as const,
  seoProfile: (workspaceId: string) => ["workspaces", workspaceId, "seo"] as const,
  openSeo: (workspaceId: string) => ["workspaces", workspaceId, "seo", "openseo"] as const,
  opportunities: (workspaceId: string) => ["workspaces", workspaceId, "opportunities"] as const,
  opportunityList: (workspaceId: string, filter: Paged<{ status: OpportunityStatus | "all" }>) =>
    ["workspaces", workspaceId, "opportunities", "list", filter] as const,
  templates: (workspaceId: string) => ["workspaces", workspaceId, "templates"] as const,
  template: (workspaceId: string, templateId: string) =>
    ["workspaces", workspaceId, "templates", templateId] as const,
  job: (workspaceId: string, jobId: string) => ["workspaces", workspaceId, "jobs", jobId] as const,
  notifications: (workspaceId: string) => ["workspaces", workspaceId, "notifications"] as const,
  notificationList: (workspaceId: string, filter: Paged<object>) =>
    ["workspaces", workspaceId, "notifications", "list", filter] as const,
  invitation: (token: string) => ["invitation", token] as const,
  workspaces: () => ["workspaces"] as const,
  workspace: (workspaceId: string) => ["workspaces", workspaceId] as const,
  members: (workspaceId: string) => ["workspaces", workspaceId, "members"] as const,
  invitations: (workspaceId: string) => ["workspaces", workspaceId, "invitations"] as const,
  pipelines: (workspaceId: string) => ["workspaces", workspaceId, "pipelines"] as const,
  pipeline: (workspaceId: string, pipelineId: string) =>
    ["workspaces", workspaceId, "pipelines", pipelineId] as const,
  upcomingRuns: (workspaceId: string, days: number) =>
    ["workspaces", workspaceId, "upcoming-runs", days] as const,
  documents: (workspaceId: string) => ["workspaces", workspaceId, "documents"] as const,
  // Both document lists sit under "list", so refreshing them leaves each
  // document's own queries (detail, pages, file) alone.
  documentLists: (workspaceId: string) => ["workspaces", workspaceId, "documents", "list"] as const,
  documentsPaged: (workspaceId: string) =>
    ["workspaces", workspaceId, "documents", "list", "paged"] as const,
  documentList: (workspaceId: string, filter: Paged<object>) =>
    ["workspaces", workspaceId, "documents", "list", "paged", filter] as const,
  allDocuments: (workspaceId: string) =>
    ["workspaces", workspaceId, "documents", "list", "all"] as const,
  document: (workspaceId: string, documentId: string) =>
    ["workspaces", workspaceId, "documents", documentId] as const,
  documentPages: (workspaceId: string, documentId: string, chunk: number) =>
    ["workspaces", workspaceId, "documents", documentId, "pages", chunk] as const,
  documentContents: (workspaceId: string, documentId: string, version: string) =>
    ["workspaces", workspaceId, "documents", documentId, "contents", version] as const,
  library: (workspaceId: string) => ["workspaces", workspaceId, "library"] as const,
  // Lists sit under "list", so refreshing them leaves each asset's own
  // query alone.
  libraryLists: (workspaceId: string) => ["workspaces", workspaceId, "library", "list"] as const,
  libraryList: (workspaceId: string, filter: Paged<{ kind: LibraryAssetKind | "all"; archived: boolean }>) =>
    ["workspaces", workspaceId, "library", "list", filter] as const,
  libraryAsset: (workspaceId: string, assetId: string) =>
    ["workspaces", workspaceId, "library", assetId] as const,
  jobs: (workspaceId: string) => ["workspaces", workspaceId, "jobs"] as const,
  jobList: (workspaceId: string, filter: Paged<{ status: JobStatus[]; kind: string[] }>) =>
    ["workspaces", workspaceId, "jobs", "list", filter] as const,
  stories: (workspaceId: string) => ["workspaces", workspaceId, "stories"] as const,
  storyLists: (workspaceId: string) => ["workspaces", workspaceId, "stories", "list"] as const,
  storiesPaged: (workspaceId: string) => ["workspaces", workspaceId, "stories", "list", "paged"] as const,
  storyList: (workspaceId: string, filter: Paged<{ status: DeliverableStatus[] }>) =>
    ["workspaces", workspaceId, "stories", "list", "paged", filter] as const,
  storyWindows: (workspaceId: string) => ["workspaces", workspaceId, "stories", "list", "window"] as const,
  storyWindow: (workspaceId: string, status: DeliverableStatus[]) =>
    ["workspaces", workspaceId, "stories", "list", "window", status] as const,
  story: (workspaceId: string, storyId: string) =>
    ["workspaces", workspaceId, "stories", storyId] as const,
  revisions: (workspaceId: string, storyId: string, deliverableId: string) =>
    ["workspaces", workspaceId, "stories", storyId, "deliverables", deliverableId, "revisions"] as const,
  content: (workspaceId: string, storyId: string, deliverableId: string) =>
    ["workspaces", workspaceId, "stories", storyId, "deliverables", deliverableId, "content"] as const,
  moments: (workspaceId: string, storyId: string, deliverableId: string) =>
    ["workspaces", workspaceId, "stories", storyId, "deliverables", deliverableId, "moments"] as const,
  artifact: (
    workspaceId: string,
    storyId: string,
    deliverableId: string,
    revision: number,
    name: string
  ) =>
    [
      "workspaces",
      workspaceId,
      "stories",
      storyId,
      "deliverables",
      deliverableId,
      "revisions",
      revision,
      name,
    ] as const,
}
