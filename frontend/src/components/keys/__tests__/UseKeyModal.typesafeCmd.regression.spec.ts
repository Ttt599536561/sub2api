import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import UseKeyModal from '../UseKeyModal.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('file-saver', () => ({ saveAs: vi.fn() }))

function mountModal() {
  return mount(UseKeyModal, {
    props: { show: true, apiKey: 'sk-typesafe-test', baseUrl: 'https://example.com/v1', platform: 'typesafe' },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: { template: '<span />' } } }
  })
}

describe('TypeSafe shell examples', () => {
  it('preserves escaped JSON quotes in the generated Windows CMD example on every platform', async () => {
    const wrapper = mountModal()
    try {
      const shellTab = wrapper.findAll('button').find(button => button.text().trim() === 'Windows CMD')
      expect(shellTab).toBeDefined()
      await shellTab!.trigger('click')
      expect(wrapper.find('pre code').text()).toContain(
        String.raw`--data "{\"model\":\"jev-latest\",\"state\":\"Text to evaluate\",\"questions\":{\"safety\":{\"type\":\"noul\",\"instructions\":\"Evaluate whether the text is unsafe\"}}}"`
      )
    } finally {
      wrapper.unmount()
    }
  })

  it.runIf(process.platform === 'win32')('passes one valid JSON body through the actual Windows CMD parser', async () => {
    const wrapper = mountModal()
    const directory = mkdtempSync(join(tmpdir(), 'sub2api-typesafe-cmd-'))
    try {
      const shellTab = wrapper.findAll('button').find(button => button.text().trim() === 'Windows CMD')
      expect(shellTab).toBeDefined()
      await shellTab!.trigger('click')
      const command = wrapper.find('pre code').text()
      const capture = join(directory, 'capture.cjs')
      const batch = join(directory, 'capture.cmd')
      writeFileSync(capture, 'process.stdout.write(JSON.stringify(process.argv.slice(2)))')
      // Replace only curl itself with an argv recorder. CMD still parses the
      // generated URL, headers, line continuations and request body verbatim.
      writeFileSync(batch, '@echo off\r\n' + command.replace(/^curl\b/, `"${process.execPath}" "${capture}"`).replace(/\r?\n/g, '\r\n'))
      const args = JSON.parse(execFileSync('cmd.exe', ['/d', '/c', batch], { encoding: 'utf8' })) as string[]
      const body = args[args.indexOf('--data') + 1]
      expect(JSON.parse(body)).toEqual({
        model: 'jev-latest', state: 'Text to evaluate',
        questions: { safety: { type: 'noul', instructions: 'Evaluate whether the text is unsafe' } }
      })
      expect(args.at(-1)).toBe(body)
    } finally {
      wrapper.unmount()
      rmSync(directory, { recursive: true, force: true })
    }
  })
})
