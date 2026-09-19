// Money stays decimal text through the UI and transport. BigInt is only used
// for exact input validation; balances used for transfers come from the server.
export function moneyCents(value: string): bigint | null {
  const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(value.trim())
  if (!match) return null
  return BigInt(match[1]) * 100n + BigInt((match[2] || '').padEnd(2, '0'))
}
export function welfareMoney(value: string | undefined): string {
  if (value === undefined) return '—'
  const negative = value.startsWith('-')
  const unsigned = negative ? value.slice(1) : value
  const [whole, fraction = ''] = unsigned.split('.')
  return `${negative ? '−' : ''}$${whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',')}.${fraction.replace(/0+$/, '').padEnd(2, '0')}`
}
