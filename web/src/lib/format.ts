import { m } from "@/i18n"

const integer = new Intl.NumberFormat(m.locale, { maximumFractionDigits: 0 })
const oneDecimal = new Intl.NumberFormat(m.locale, { maximumFractionDigits: 1 })

/** 1234 → "1,234". */
export function formatInteger(n: number): string {
  return integer.format(n)
}

/** 950 → "950", 12_431 → "12.4k", 124_000 → "124k", 1_250_000 → "1.3M". */
export function formatCompact(n: number): string {
  const abs = Math.abs(n)
  if (abs < 1_000) return integer.format(n)
  if (abs < 1_000_000) {
    const k = n / 1_000
    return (Math.abs(k) >= 100 ? integer : oneDecimal).format(k) + m.units.thousand
  }
  return oneDecimal.format(n / 1_000_000) + m.units.million
}

/** 2_400_000 → "2.3 MB". */
export function formatBytes(n: number): string {
  const units = m.units.bytes
  let value = n
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  const f = unit === 0 || value >= 100 ? integer : oneDecimal
  return `${f.format(value)} ${units[unit]}`
}

/** Up to two initials for an avatar: "Hakan Beşel" → "HB", "acme" → "ps". */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return "?"
  if (words.length === 1) return [...words[0]].slice(0, 2).join("")
  return words
    .slice(0, 2)
    .map((w) => [...w][0])
    .join("")
    .toLocaleUpperCase()
}
