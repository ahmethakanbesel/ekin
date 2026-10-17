import { useRef, useState } from "react"

import type { UploadOptions } from "@/api/upload"
import { errorMessage } from "@/lib/errors"

export type UploadItem = {
  key: string
  name: string
  size: number
  state: "waiting" | "uploading" | "failed"
  /** 0–1 of the request body sent. */
  progress: number
  error?: string
}

let nextKey = 0

/**
 * A queue of files uploaded one at a time. Finished uploads leave the
 * queue (the item shows up in its list, still processing); failed ones
 * stay with their error until dismissed. `precheck` refuses a file before
 * it is sent, with the reason.
 */
export function useUploadQueue({
  upload,
  precheck,
}: {
  upload: (file: File, opts: UploadOptions) => Promise<unknown>
  precheck: (file: File) => string | undefined
}) {
  const [items, setItems] = useState<UploadItem[]>([])
  const queue = useRef<Array<{ key: string; file: File }>>([])
  const running = useRef(false)
  const current = useRef<{ key: string; ctrl: AbortController } | null>(null)

  const patch = (key: string, p: Partial<UploadItem>) =>
    setItems((xs) => xs.map((x) => (x.key === key ? { ...x, ...p } : x)))
  const drop = (key: string) =>
    setItems((xs) => xs.filter((x) => x.key !== key))

  const pump = async () => {
    if (running.current) return
    running.current = true
    try {
      for (
        let next = queue.current.shift();
        next;
        next = queue.current.shift()
      ) {
        const { key, file } = next
        const ctrl = new AbortController()
        current.current = { key, ctrl }
        patch(key, { state: "uploading", progress: 0 })
        try {
          await upload(file, {
            signal: ctrl.signal,
            onProgress: (progress) => patch(key, { progress }),
          })
          drop(key)
        } catch (err) {
          if (ctrl.signal.aborted) drop(key)
          else patch(key, { state: "failed", error: errorMessage(err) })
        }
      }
    } finally {
      running.current = false
      current.current = null
    }
  }

  const add = (files: Iterable<File>) => {
    const added: UploadItem[] = []
    for (const file of files) {
      const key = `upload-${++nextKey}`
      const error = precheck(file)
      added.push({
        key,
        name: file.name,
        size: file.size,
        state: error ? "failed" : "waiting",
        progress: 0,
        error,
      })
      if (!error) queue.current.push({ key, file })
    }
    if (added.length === 0) return
    setItems((xs) => [...xs, ...added])
    void pump()
  }

  /** Stops an upload in flight, or takes a waiting one off the queue. */
  const cancel = (key: string) => {
    if (current.current?.key === key) {
      current.current.ctrl.abort()
      return
    }
    queue.current = queue.current.filter((q) => q.key !== key)
    drop(key)
  }

  return { items, add, cancel, dismiss: drop }
}

/** The lowercase extension of a file name, with its dot ("" for none). */
export function extensionOf(name: string): string {
  const dot = name.lastIndexOf(".")
  return dot >= 0 ? name.slice(dot).toLowerCase() : ""
}
