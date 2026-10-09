export type WelfareDatePreset = 'today' | 'yesterday' | 'last7' | 'last30' | 'month' | 'custom'
const dayMilliseconds = 86_400_000

export function shanghaiBusinessDate(now = new Date()): string {
  const parts = new Intl.DateTimeFormat('en', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  const part = (type: string) => parts.find(value => value.type === type)?.value
  return `${part('year')}-${part('month')}-${part('day')}`
}

export function addBusinessDays(date: string, days: number): string {
  return new Date(Date.parse(`${date}T00:00:00Z`) + days * dayMilliseconds).toISOString().slice(0, 10)
}

export function welfareDateRange(preset: Exclude<WelfareDatePreset, 'custom'>, now = new Date()) {
  const today = shanghaiBusinessDate(now)
  if (preset === 'yesterday') { const yesterday = addBusinessDays(today, -1); return { date_from: yesterday, date_to: yesterday } }
  const from = preset === 'last7' ? addBusinessDays(today, -6)
    : preset === 'last30' ? addBusinessDays(today, -29)
      : preset === 'month' ? `${today.slice(0, 7)}-01` : today
  return { date_from: from, date_to: today }
}

export function validWelfareDateRange(from: string, to: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(from) || !/^\d{4}-\d{2}-\d{2}$/.test(to)) return false
  if (Number(from.slice(0, 4)) < 1 || Number(to.slice(0, 4)) < 1) return false
  const start = Date.parse(`${from}T00:00:00Z`)
  const end = Date.parse(`${to}T00:00:00Z`)
  if (!Number.isFinite(start) || !Number.isFinite(end)) return false
  if (new Date(start).toISOString().slice(0, 10) !== from || new Date(end).toISOString().slice(0, 10) !== to) return false
  const days = (end - start) / dayMilliseconds + 1
  return days >= 1 && days <= 366
}
