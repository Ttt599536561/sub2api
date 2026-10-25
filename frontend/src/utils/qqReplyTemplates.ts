import zh from '@/i18n/locales/zh/qqImage'
import en from '@/i18n/locales/en/qqImage'

type ReplyVariableInfo = { label: string; description: string }

function messages(locale: string) {
  return locale.toLowerCase().startsWith('zh') ? zh.qqImage.admin.templates : en.qqImage.admin.templates
}

export function replyVariableInfo(name: string, locale: string): ReplyVariableInfo {
  const source = messages(locale)
  const definitions: Record<string, ReplyVariableInfo> = source.variableInfo
  if (Object.prototype.hasOwnProperty.call(definitions, name)) return { ...definitions[name]! }
  return { label: name, description: source.unknownVariableDescription }
}

// Replace complete labels in one pass. Do not reinterpret replacement text or
// repair malformed braces: the existing server validator must see those intact.
function replaceLabels(text: string, replace: (name: string) => string | undefined): string {
  return text.replace(/\{\{([^{}\r\n]*)\}\}/g, (token: string, name: string, offset: number) => {
    if (text[offset - 1] === '{' || text[offset + token.length] === '}') return token
    const result = replace(name)
    return result === undefined ? token : `{{${result}}}`
  })
}

export function toDisplayReplyText(text: string, allowed: string[], locale: string): string {
  const allowedNames = new Set(allowed)
  return replaceLabels(text, name => allowedNames.has(name) ? replyVariableInfo(name, locale).label : undefined)
}

export function toStoredReplyText(text: string, allowed: string[], locale: string): string {
  const allowedNames = new Set(allowed)
  const namesByLabel = new Map<string, string | undefined>()
  for (const name of allowedNames) {
    const label = replyVariableInfo(name, locale).label
    // A future duplicate label must never silently select the wrong variable.
    namesByLabel.set(label, namesByLabel.has(label) ? undefined : name)
  }
  return replaceLabels(text, name => allowedNames.has(name) ? name : namesByLabel.get(name))
}
