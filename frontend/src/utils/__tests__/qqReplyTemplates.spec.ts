import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { replyVariableInfo, toDisplayReplyText, toStoredReplyText } from '../qqReplyTemplates'

const backendCatalog = readFileSync(resolve(process.cwd(), '../backend/internal/qqimage/reply_templates.go'), 'utf8')
const catalogVariables = [...new Set([...backendCatalog.matchAll(/"([a-z][a-z0-9_]*)":\s*\d+/g)].map(match => match[1]!))]
const defaultTemplates = [...backendCatalog.matchAll(/replyDefinition\("[^"]+", "[^"]+", "[^"]+", ("(?:[^"\\]|\\.)*")/g)]
  .map(match => JSON.parse(match[1]!) as string)

describe('QQ reply template display text', () => {
  it('shows understandable Chinese names without changing ordinary text', () => {
    const source = '你好 👩‍💻\r\n智商查询指令：\n{{iq_commands}}\n{{group_names}}'
    expect(toDisplayReplyText(source, ['iq_commands', 'group_names'], 'zh-CN'))
      .toBe('你好 👩‍💻\r\n智商查询指令：\n{{智商查询指令}}\n{{可用分组名称}}')
    expect(replyVariableInfo('iq_commands', 'zh').description).toContain('示例')
  })

  it('explains every variable in the actual backend catalog with unique labels in both languages', () => {
    expect(catalogVariables).toHaveLength(30)
    for (const locale of ['zh', 'en']) {
      const labels = catalogVariables.map(name => {
        const info = replyVariableInfo(name, locale)
        expect(info.label, name).not.toBe(name)
        expect(info.description.length, name).toBeGreaterThan(5)
        expect(info.description).not.toContain('{{')
        return info.label
      })
      expect(new Set(labels).size).toBe(catalogVariables.length)
    }
  })

  it('round-trips every existing default and every catalog variable unchanged', () => {
    expect(defaultTemplates.length).toBeGreaterThan(15)
    const texts = [...defaultTemplates, catalogVariables.map(name => `{{${name}}}`).join('\n')]
    for (const locale of ['zh', 'zh-CN', 'en', 'en-US']) {
      for (const source of texts) {
        expect(toStoredReplyText(toDisplayReplyText(source, catalogVariables, locale), catalogVariables, locale)).toBe(source)
      }
    }
  })

  it('converts only variables allowed in the current scenario', () => {
    const source = '{{iq_commands}} / {{group_names}} / {{future_variable}}'
    expect(toDisplayReplyText(source, ['iq_commands'], 'zh'))
      .toBe('{{智商查询指令}} / {{group_names}} / {{future_variable}}')
    expect(toStoredReplyText('{{智商查询指令}} / {{可用分组名称}} / {{future_variable}}', ['iq_commands'], 'zh'))
      .toBe('{{iq_commands}} / {{可用分组名称}} / {{future_variable}}')
  })

  it('keeps unknown and inherited object names intact with a readable fallback', () => {
    for (const name of ['future_variable', '__proto__', 'constructor', 'toString']) {
      const token = `{{${name}}}`
      expect(replyVariableInfo(name, 'zh').label).toBe(name)
      expect(replyVariableInfo(name, 'en').description).toContain('automatically')
      expect(toDisplayReplyText(token, [name], 'zh')).toBe(token)
      expect(toStoredReplyText(token, [name], 'zh')).toBe(token)
    }
  })

  it('does not interpret malformed, nested, spaced or multiline placeholders', () => {
    const source = '{{{iq_commands}}} {{ iq_commands }} {{iq_commands\n}} {iq_commands} {{智商查询指令\n}}'
    expect(toDisplayReplyText(source, ['iq_commands'], 'zh')).toBe(source)
    expect(toStoredReplyText(source, ['iq_commands'], 'zh')).toBe(source)
  })

  it('converts each token once and preserves repetitions for required-content validation', () => {
    const shown = '{{智商查询指令}}{{智商查询指令}} {{iq_commands}} 智商查询指令'
    expect(toStoredReplyText(shown, ['iq_commands'], 'zh'))
      .toBe('{{iq_commands}}{{iq_commands}} {{iq_commands}} 智商查询指令')
    expect(toDisplayReplyText('{{iq_commands}}{{iq_commands}} {{智商查询指令}}', ['iq_commands'], 'zh'))
      .toBe('{{智商查询指令}}{{智商查询指令}} {{智商查询指令}}')
  })

  it('switches display language from canonical text without mutating saved content', () => {
    const canonical = '🌸 {{iq_commands}}\n{{price}}'
    const allowed = ['iq_commands', 'price']
    const chinese = toDisplayReplyText(canonical, allowed, 'zh')
    const restored = toStoredReplyText(chinese, allowed, 'zh')
    const english = toDisplayReplyText(restored, allowed, 'en')
    expect(english).toContain('{{IQ query commands}}')
    expect(english).not.toContain('智商查询指令')
    expect(toStoredReplyText(english, allowed, 'en')).toBe(canonical)
    expect(toStoredReplyText(canonical, allowed, 'zh')).toBe(canonical)
  })
})
