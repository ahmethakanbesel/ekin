import { LibrarySettingsCard } from "@/features/library/settings-card"
import { OpenSeoCard } from "@/features/seo/openseo-card"
import { SeoProfileCard } from "@/features/seo/profile-card"
import { useCan } from "@/features/workspaces/context"
import { SettingsSectionHeader } from "./settings-page"

// Sections whose cards carry their own read-only notes.

/** Settings › Library: what the library keeps from finished drafts. */
export function LibrarySection() {
  return (
    <div className="max-w-3xl">
      <SettingsSectionHeader section="library" />
      <LibrarySettingsCard />
    </div>
  )
}

/** Settings › SEO: the OpenSEO connection (owners) and the site profile research uses. */
export function SeoSection() {
  const canManage = useCan("manage")
  return (
    <div className="max-w-3xl">
      <SettingsSectionHeader section="seo" />
      <div className="grid gap-6">
        {canManage ? <OpenSeoCard /> : null}
        <SeoProfileCard />
      </div>
    </div>
  )
}
