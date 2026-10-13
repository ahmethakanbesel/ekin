import type { JobStatus } from "@/api/client"

// Hierarchical keys: invalidating ["workspaces", id] also refreshes that
// workspace's pipelines, jobs and members.
export const keys = {
  me: () => ["me"] as const,
  invitation: (token: string) => ["invitation", token] as const,
  workspaces: () => ["workspaces"] as const,
  workspace: (workspaceId: string) => ["workspaces", workspaceId] as const,
  members: (workspaceId: string) => ["workspaces", workspaceId, "members"] as const,
  invitations: (workspaceId: string) => ["workspaces", workspaceId, "invitations"] as const,
  pipelines: (workspaceId: string) => ["workspaces", workspaceId, "pipelines"] as const,
  pipeline: (workspaceId: string, pipelineId: string) =>
    ["workspaces", workspaceId, "pipelines", pipelineId] as const,
  jobs: (workspaceId: string) => ["workspaces", workspaceId, "jobs"] as const,
  jobList: (workspaceId: string, filter: { status: JobStatus[]; limit: number }) =>
    ["workspaces", workspaceId, "jobs", "list", filter] as const,
}
