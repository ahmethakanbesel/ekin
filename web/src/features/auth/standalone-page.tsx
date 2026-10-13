import type { ReactNode } from "react"

import { Brand } from "@/components/brand"
import { m } from "@/i18n"
import { UserMenu } from "./user-menu"

/** A signed-in page outside any workspace: the brand, the user menu, content. */
export function StandalonePage({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-svh flex-col">
      <header className="flex items-center gap-2 border-b bg-sidebar p-3">
        <Brand />
        <div className="ml-auto">
          <UserMenu compact />
        </div>
      </header>
      <main className="mx-auto w-full max-w-3xl flex-1 p-4 md:p-8">{children}</main>
    </div>
  )
}

/** A signed-out page: a centred column under the product name. */
export function GuestPage({ children }: { children: ReactNode }) {
  return (
    <main className="flex min-h-svh items-center justify-center p-4">
      <div className="w-full max-w-md space-y-6">
        <p className="text-center font-heading text-xl font-semibold tracking-tight">
          {m.app.name}
        </p>
        {children}
      </div>
    </main>
  )
}
