import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
})

describe('BaseDialog optional focus trap', () => {
  const wrappers: ReturnType<typeof mount>[] = []
  function create(props: Record<string, unknown> = {}, slots = { default: '<input id="dialog-first" /><button id="dialog-last">Confirm</button>' }) {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details', ...props },
      slots,
      global: { stubs: { Icon: true } }
    })
    wrappers.push(wrapper)
    return wrapper
  }
  function key(key: string, shiftKey = false) {
    const event = new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })
    document.activeElement!.dispatchEvent(event)
    return event
  }
  afterEach(() => {
    wrappers.splice(0).reverse().forEach(wrapper => wrapper.unmount())
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('keeps focus trapping and background inert opt-in', async () => {
    const background = document.createElement('main')
    document.body.append(background)
    const wrapper = create()
    await wrapper.setProps({ show: true })
    await nextTick()
    document.querySelector<HTMLElement>('#dialog-last')!.focus()
    expect(key('Tab').defaultPrevented).toBe(false)
    expect(background.hasAttribute('inert')).toBe(false)
  })

  it('wraps both Tab directions and excludes unavailable controls', async () => {
    const wrapper = create({ trapFocus: true, showCloseButton: false }, {
      default: '<button disabled>Disabled</button><div hidden><button>Hidden</button></div><input id="dialog-first" /><button id="dialog-last">Confirm</button><button tabindex="-1">Skip</button>'
    })
    await wrapper.setProps({ show: true })
    await nextTick()
    const first = document.querySelector<HTMLElement>('#dialog-first')!
    const last = document.querySelector<HTMLElement>('#dialog-last')!
    expect(document.activeElement).toBe(first)
    first.focus()
    expect(key('Tab', true).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(last)
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(first)
  })

  it('contains focus in the panel when there are no available controls', async () => {
    const wrapper = create({ trapFocus: true, showCloseButton: false }, { default: '<button disabled>Waiting</button>' })
    await wrapper.setProps({ show: true })
    await nextTick()
    const panel = document.querySelector('.modal-content')!
    expect(document.activeElement).toBe(panel)
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(panel)
  })

  it('makes the background inert and restores prior inert state and launcher focus', async () => {
    const background = document.createElement('main')
    background.innerHTML = '<button id="launcher">Open</button>'
    const alreadyInert = document.createElement('aside')
    alreadyInert.setAttribute('inert', 'existing')
    document.body.append(background, alreadyInert)
    const launcher = document.querySelector<HTMLElement>('#launcher')!
    launcher.focus()
    const wrapper = create({ trapFocus: true })
    await wrapper.setProps({ show: true })
    await nextTick()
    expect(background.hasAttribute('inert')).toBe(true)
    launcher.focus()
    expect(document.querySelector('.modal-content')!.contains(document.activeElement)).toBe(true)
    await wrapper.setProps({ show: false })
    expect(background.hasAttribute('inert')).toBe(false)
    expect(alreadyInert.getAttribute('inert')).toBe('existing')
    expect(document.activeElement).toBe(launcher)
    await wrapper.setProps({ show: true })
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(background.hasAttribute('inert')).toBe(false)
    expect(document.activeElement).toBe(launcher)
    expect(key('Tab').defaultPrevented).toBe(false)
  })

  it('lets only the top nested dialog trap focus and handle Escape', async () => {
    const background = document.createElement('main')
    document.body.append(background)
    const parent = create({ trapFocus: true }, { default: '<button id="open-child">Open child</button>' })
    await parent.setProps({ show: true })
    await nextTick()
    const trigger = document.querySelector<HTMLElement>('#open-child')!
    trigger.focus()
    const child = create({ trapFocus: true, zIndex: 60 }, { default: '<button id="child-last">Finish</button>' })
    await child.setProps({ show: true })
    await nextTick()
    const overlays = document.querySelectorAll<HTMLElement>('.modal-overlay')
    expect(overlays[0].hasAttribute('inert')).toBe(true)
    expect(overlays[1].hasAttribute('inert')).toBe(false)
    document.querySelector<HTMLElement>('#child-last')!.focus()
    key('Tab')
    expect(overlays[1].contains(document.activeElement)).toBe(true)
    key('Escape')
    expect(child.emitted('close')).toHaveLength(1)
    expect(parent.emitted('close')).toBeUndefined()
    await child.setProps({ show: false })
    expect(overlays[0].hasAttribute('inert')).toBe(false)
    expect(background.hasAttribute('inert')).toBe(true)
    expect(document.activeElement).toBe(trigger)
  })

  it('suspends the underlying trap while a default dialog is on top', async () => {
    const parent = create({ trapFocus: true })
    await parent.setProps({ show: true })
    const child = create({ zIndex: 60 }, { default: '<button id="child-only">Child</button>' })
    await child.setProps({ show: true })
    await nextTick()
    const button = document.querySelector<HTMLElement>('#child-only')!
    button.focus()
    expect(document.activeElement).toBe(button)
    expect(key('Tab').defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(button)
  })
})
