import type { DeliverableStatus, JobStatus, OpportunityStatus } from "@/api/client"

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
  opportunityList: (workspaceId: string, status: OpportunityStatus | "all") =>
    ["workspaces", workspaceId, "opportunities", "list", status] as const,
  templates: (workspaceId: string) => ["workspaces", workspaceId, "templates"] as const,
  template: (workspaceId: string, templateId: string) =>
    ["workspaces", workspaceId, "templates", templateId] as const,
  job: (workspaceId: string, jobId: string) => ["workspaces", workspaceId, "jobs", jobId] as const,
  notifications: (workspaceId: string) => ["workspaces", workspaceId, "notifications"] as const,
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
  document: (workspaceId: string, documentId: string) =>
    ["workspaces", workspaceId, "documents", documentId] as const,
  documentPages: (workspaceId: string, documentId: string, chunk: number) =>
    ["workspaces", workspaceId, "documents", documentId, "pages", chunk] as const,
  documentContents: (workspaceId: string, documentId: string, version: string) =>
    ["workspaces", workspaceId, "documents", documentId, "contents", version] as const,
  jobs: (workspaceId: string) => ["workspaces", workspaceId, "jobs"] as const,
  jobList: (workspaceId: string, filter: { status: JobStatus[]; limit: number }) =>
    ["workspaces", workspaceId, "jobs", "list", filter] as const,
  stories: (workspaceId: string) => ["workspaces", workspaceId, "stories"] as const,
  storyLists: (workspaceId: string) => ["workspaces", workspaceId, "stories", "list"] as const,
  storyList: (workspaceId: string, filter: { status: DeliverableStatus[]; limit: number }) =>
    ["workspaces", workspaceId, "stories", "list", filter] as const,
  story: (workspaceId: string, storyId: string) =>
    ["workspaces", workspaceId, "stories", storyId] as const,
  revisions: (workspaceId: string, storyId: string, deliverableId: string) =>
    ["workspaces", workspaceId, "stories", storyId, "deliverables", deliverableId, "revisions"] as const,
  content: (workspaceId: string, storyId: string, deliverableId: string) =>
    ["workspaces", workspaceId, "stories", storyId, "deliverables", deliverableId, "content"] as const,
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
