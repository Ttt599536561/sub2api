import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { compileStyle, parse } from 'vue/compiler-sfc'
import postcss from 'postcss'

function compiledStyles(filename: string) {
  const source = readFileSync(fileURLToPath(new URL('../' + filename, import.meta.url)), 'utf8')
  const result = compileStyle({
    filename,
    source: parse(source).descriptor.styles[0].content,
    scoped: true,
    id: 'data-v-card-visual',
  })
  expect(result.errors).toEqual([])
  return postcss.parse(result.code)
}

function declaration(filename: string, selector: string, property: string) {
  let value: string | undefined
  compiledStyles(filename).walkRules(rule => {
    if (rule.selector !== selector) return
    rule.walkDecls(property, item => { value ??= item.value })
  })
  return value
}

describe('compiled monitor card styles', () => {
  it.each([
    ['MonitorCard.vue', '.dark .monitor-card[data-v-card-visual]'],
    ['MonitorCards.vue', '.dark .monitor-provider-heading h2[data-v-card-visual]'],
    ['MonitorBars.vue', '.dark .monitor-bars-tooltip[data-v-card-visual]'],
  ])('%s keeps dark theme styles on the component rather than the document', (filename, expectedSelector) => {
    const selectors: string[] = []
    compiledStyles(filename).walkRules(rule => { selectors.push(...rule.selector.split(',').map(selector => selector.trim())) })
    expect(selectors).toContain(expectedSelector)
    expect(selectors).not.toContain('.dark')
  })

  it('uses an explicit compact monospace family for metric values', () => {
    const selector = '.monitor-card-metrics dd[data-v-card-visual]'
    expect(declaration('MonitorCard.vue', selector, 'font-family')).toContain('Consolas')
    expect(declaration('MonitorCard.vue', selector, 'font-family')).toContain('monospace')
    expect(declaration('MonitorCard.vue', selector, 'letter-spacing')).toBe('normal')
  })

  it('encodes states by height while keeping the hover target at full size', () => {
    const selector = '.monitor-bar[data-v-card-visual]'
    expect(declaration('MonitorBars.vue', selector, 'height')).toBe('1.5rem')
    expect(declaration('MonitorBars.vue', selector, 'align-items')).toBe('flex-end')
    for (const [state, height] of Object.entries({ healthy: '100%', warning: '65%', critical: '35%', unknown: '40%', 'no-data': '15%' })) {
      expect(declaration('MonitorBars.vue', '.monitor-bar[data-health="' + state + '"][data-v-card-visual]', '--bar-height')).toBe(height)
    }
    expect(declaration('MonitorBars.vue', '.monitor-bar-fill[data-v-card-visual]', 'pointer-events')).toBe('none')
    expect(declaration('MonitorBars.vue', '.monitor-bar[data-v-card-visual]:hover', 'transform')).toBeUndefined()
    expect(declaration('MonitorBars.vue', '.monitor-bar:hover .monitor-bar-fill[data-v-card-visual]', 'transform')).toContain('scale')
  })
})
