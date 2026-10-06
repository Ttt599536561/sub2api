export const DEFAULT_PAYMENT_CURRENCY = 'CNY'

const PAYMENT_CURRENCY_SYMBOLS: Record<string, string> = {
  USD: '$',
  CNY: '¥',
  RMB: '¥',
  EUR: '€',
  GBP: '£',
  JPY: '¥',
  HKD: 'HK$',
  TWD: 'NT$',
  KRW: '₩',
  AUD: 'A$',
  CAD: 'C$',
  SGD: 'S$',
  NZD: 'NZ$',
  MOP: 'MOP$',
  MYR: 'RM',
  THB: '฿',
  PHP: '₱',
  INR: '₹',
}

export function normalizePaymentCurrency(currency?: string | null): string {
  const normalized = String(currency || '').trim().toUpperCase()
  return /^[A-Z]{3}$/.test(normalized) ? normalized : DEFAULT_PAYMENT_CURRENCY
}

export function currencySymbol(currency?: string | null): string {
  const normalized = normalizePaymentCurrency(currency)
  return PAYMENT_CURRENCY_SYMBOLS[normalized] || normalized
}

// Keep payment precision aligned with backend/internal/payment/currency.go;
// browser CLDR defaults differ for currencies such as IQD and CLF.
const ZERO_DECIMAL_CURRENCIES = new Set(['BIF', 'CLP', 'DJF', 'GNF', 'JPY', 'KMF', 'KRW', 'MGA', 'PYG', 'RWF', 'VND', 'VUV', 'XAF', 'XOF', 'XPF', 'ISK', 'UGX'])
const THREE_DECIMAL_CURRENCIES = new Set(['BHD', 'IQD', 'JOD', 'KWD', 'LYD', 'OMR', 'TND'])

export function paymentCurrencyFractionDigits(currency?: string | null): number {
  const normalized = normalizePaymentCurrency(currency)
  if (ZERO_DECIMAL_CURRENCIES.has(normalized)) return 0
  if (THREE_DECIMAL_CURRENCIES.has(normalized)) return 3
  return 2
}

// Match the backend decimal.NewFromFloat inputs before multiplying: rounding
// an already-computed binary float can incorrectly add a cent (1 * 7% -> 0.08).
export function calculatePaymentFee(amount: number, feeRate: number, currency?: string | null): number {
  if (!Number.isFinite(amount) || !Number.isFinite(feeRate) || amount <= 0 || feeRate <= 0) return 0
  const decimalParts = (value: number) => {
    const [mantissa, exponent = '0'] = String(value).split('e')
    const [whole, fraction = ''] = mantissa.split('.')
    return { coefficient: BigInt(whole + fraction), scale: fraction.length - Number(exponent) }
  }
  const amountParts = decimalParts(amount)
  const rateParts = decimalParts(feeRate)
  const digits = paymentCurrencyFractionDigits(normalizePaymentCurrency(currency))
  const coefficient = amountParts.coefficient * rateParts.coefficient
  // shopspring/decimal.Div first rounds to 16 fractional digits, then the
  // backend rounds the positive fee upward to the currency's minor unit.
  const divisionPrecision = 16
  const power = divisionPrecision - amountParts.scale - rateParts.scale - 2
  let scaledFee: bigint
  if (power >= 0) scaledFee = coefficient * 10n ** BigInt(power)
  else {
    const divisionScale = 10n ** BigInt(-power)
    scaledFee = (coefficient + divisionScale / 2n) / divisionScale
  }
  const divisor = 10n ** BigInt(divisionPrecision - digits)
  const minorUnits = (scaledFee + divisor - 1n) / divisor
  return Number(minorUnits) / 10 ** digits
}

export function formatPaymentAmount(amount: number, currency?: string | null, locale?: string): string {
  const normalized = normalizePaymentCurrency(currency)
  const fractionDigits = paymentCurrencyFractionDigits(normalized)
  try {
    return new Intl.NumberFormat(locale || undefined, {
      style: 'currency',
      currency: normalized,
      currencyDisplay: 'narrowSymbol',
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(Number.isFinite(amount) ? amount : 0)
  } catch {
    return `${normalized} ${(Number.isFinite(amount) ? amount : 0).toFixed(fractionDigits)}`
  }
}
