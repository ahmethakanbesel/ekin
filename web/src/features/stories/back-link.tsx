import { Link } from "@tanstack/react-router"
import { ArrowLeftIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { m } from "@/i18n"

export function BackToStories({ workspaceId }: { workspaceId: string }) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className="mb-2 -ml-2"
      render={<Link to="/w/$workspaceId/stories" params={{ workspaceId }} />}
      nativeButton={false}
    >
      <ArrowLeftIcon data-icon="inline-start" />
      {m.nav.review}
    </Button>
  )
}
