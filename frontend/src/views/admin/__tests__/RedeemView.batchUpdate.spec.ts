import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'

import RedeemView from '../RedeemView.vue'

const { listRedeemCodes, batchUpdateRedeemCodes, generate, welfareCampaigns, getAllGroups, showSuccess, showError, showInfo } =
  vi.hoisted(() => ({
    listRedeemCodes: vi.fn(),
    batchUpdateRedeemCodes: vi.fn(),
    generate: vi.fn(),
    welfareCampaigns: vi.fn(),
    getAllGroups: vi.fn(),
    showSuccess: vi.fn(),
    showError: vi.fn(),
    showInfo: vi.fn()
  }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    redeem: {
      list: listRedeemCodes,
      generate,
      welfareCampaigns,
      delete: vi.fn(),
      batchDelete: vi.fn(),
      batchUpdate: batchUpdateRedeemCodes,
      exportCodes: vi.fn()
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError,
    showInfo
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <table>
      <thead>
        <tr>
          <th v-for="column in columns" :key="column.key">
            <slot :name="'header-' + column.key" :column="column">{{ column.label }}</slot>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in data" :key="row.id">
          <td v-for="column in columns" :key="column.key">
            <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]">
              {{ row[column.key] }}
            </slot>
          </td>
        </tr>
      </tbody>
    </table>
  `
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  setup(props: { options: Array<{ value: unknown; label: string }> }, { emit }: { emit: (event: string, ...args: unknown[]) => void }) {
    const onChange = (event: Event) => {
      const raw = (event.target as HTMLSelectElement).value
      const option = props.options.find((item) => String(item.value ?? '') === raw)
      const value = option ? option.value : raw
      emit('update:modelValue', value)
      emit('change', value, option ?? null)
    }
    return { onChange }
  },
  template: `
    <select v-bind="$attrs" :value="modelValue ?? ''" @change="onChange">
      <option v-for="option in options" :key="String(option.value ?? '')" :value="option.value ?? ''">
        {{ option.label }}
      </option>
    </select>
  `
}

describe('admin RedeemView batch update', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.innerHTML = ''

    listRedeemCodes.mockReset()
    batchUpdateRedeemCodes.mockReset()
    getAllGroups.mockReset()
    generate.mockReset()
    welfareCampaigns.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    showInfo.mockReset()

    listRedeemCodes.mockResolvedValue({
      items: [
        {
          id: 1,
          code: 'CODE-1',
          type: 'balance',
          value: 10,
          status: 'unused',
          used_by: null,
          used_at: null,
          created_at: '2026-01-01T00:00:00Z',
          expires_at: null
        },
        {
          id: 2,
          code: 'CODE-2',
          type: 'balance',
          value: 20,
          status: 'unused',
          used_by: null,
          used_at: null,
          created_at: '2026-01-01T00:00:00Z',
          expires_at: null
        }
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    batchUpdateRedeemCodes.mockResolvedValue({ updated: 1, message: 'ok' })
    getAllGroups.mockResolvedValue([])
    generate.mockResolvedValue([{ id: 3, code: 'NEW-CODE' }])
    welfareCampaigns.mockResolvedValue([{ id: 7, name: 'Autumn invitation', version: 2, subscription_group_id: 9, group_name: 'Welfare group' }])
  })

  it('submits only checked fields for selected redeem codes', async () => {
    const wrapper = mount(RedeemView, {
      attachTo: document.body,
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          Pagination: true,
          ConfirmDialog: true,
          Select: SelectStub,
          GroupBadge: true,
          GroupOptionItem: true,
          Icon: true,
          Teleport: true
        }
      }
    })

    await flushPromises()
    await wrapper.findAll('[data-test="select-code"]')[0].setValue(true)
    await wrapper.get('[data-test="batch-update-open"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="batch-field-status"]').setValue(true)
    await wrapper.get('[data-test="batch-status-select"]').setValue('disabled')
    await wrapper.get('[data-test="batch-field-notes"]').setValue(true)
    await wrapper.get('[data-test="batch-notes-input"]').setValue('maintenance')
    await wrapper.get('[data-test="batch-update-form"]').trigger('submit')
    await flushPromises()

    expect(batchUpdateRedeemCodes).toHaveBeenCalledWith([1], {
      status: 'disabled',
      notes: 'maintenance'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.redeem.batchUpdateSuccess')
  })

  async function openGenerator() {
    getAllGroups.mockResolvedValue([{ id: 8, name: 'Standard group', subscription_type: 'subscription' }])
    const wrapper = mount(RedeemView, {
      attachTo: document.body,
      global: { stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub, Pagination: true, ConfirmDialog: true, Select: SelectStub,
        GroupBadge: true, GroupOptionItem: true, Icon: true, Teleport: false
      } }
    })
    await flushPromises()
    const page = Object.assign(new DOMWrapper(document.body), { unmount: () => wrapper.unmount() })
    await page.findAll('button').find(b => b.text() === 'admin.redeem.generateCodes')!.trigger('click')
    await page.get('form select').setValue('subscription')
    await flushPromises()
    return page
  }

  it('defaults to standard subscription and preserves the original generate arguments', async () => {
    const wrapper = await openGenerator()
    expect(wrapper.get('[data-test="subscription-purpose"]').element.value).toBe('standard')
    expect(welfareCampaigns).not.toHaveBeenCalled()
    await wrapper.get('[data-test="subscription-group"]').setValue('8')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(generate).toHaveBeenCalledWith(1, 'subscription', 10, 8, 30, undefined)
    wrapper.unmount()
  })

  it('generates one-time supplement for an explicitly selected group without a campaign', async () => {
    const wrapper = await openGenerator()
    await wrapper.get('[data-test="subscription-purpose"]').setValue('add_balance')
    expect(wrapper.find('[data-test="welfare-campaign"]').exists()).toBe(false)
    expect(welfareCampaigns).not.toHaveBeenCalled()
    await wrapper.get('[data-test="subscription-group"]').setValue('8')
    await wrapper.get('[data-test="supplement-quota"]').setValue('12.50')
    await wrapper.get('[data-test="generate-count"]').setValue('3')
    expect(wrapper.text()).toContain('welfareRedeem.supplementQuotaHint')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(generate).toHaveBeenCalledWith(3, 'subscription', 0, 8, undefined, undefined, {
      subscription_effect: 'add_balance', quota_amount_usd: 12.5
    })
    wrapper.unmount()
  })

  it('switches to extension and then ordinary renewal without leaking supplement fields', async () => {
    const wrapper = await openGenerator()
    await wrapper.get('[data-test="subscription-group"]').setValue('8')
    await wrapper.get('[data-test="subscription-purpose"]').setValue('add_balance')
    await wrapper.get('[data-test="supplement-quota"]').setValue('99')
    await wrapper.get('[data-test="subscription-purpose"]').setValue('extend_subscription')
    expect(wrapper.find('[data-test="supplement-quota"]').exists()).toBe(false)
    await wrapper.get('[data-test="supplement-days"]').setValue('45')
    generate.mockRejectedValueOnce({ reason: 'SUBSCRIPTION_SUPPLEMENT_GROUP_INVALID' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(generate).toHaveBeenLastCalledWith(1, 'subscription', 0, 8, 45, undefined, {
      subscription_effect: 'extend_subscription'
    })
    expect(wrapper.text()).toContain('welfareRedeem.errors.supplementGroupUnavailable')
    await wrapper.get('[data-test="subscription-purpose"]').setValue('standard')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(generate).toHaveBeenLastCalledWith(1, 'subscription', 10, 8, 30, undefined)
    wrapper.unmount()
  })

  it('requires a selected group before generating supplements', async () => {
    const wrapper = await openGenerator()
    await wrapper.get('[data-test="subscription-purpose"]').setValue('add_balance')
    await wrapper.get('form').trigger('submit')
    expect(generate).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.redeem.groupRequired')
    wrapper.unmount()
  })

  it.each(['0', '-1', '0.0100001', '12.345', '10000000000'])(
    'rejects invalid one-time quota %s', async amount => {
      const wrapper = await openGenerator()
      await wrapper.get('[data-test="subscription-group"]').setValue('8')
      await wrapper.get('[data-test="subscription-purpose"]').setValue('add_balance')
      await wrapper.get('[data-test="supplement-quota"]').setValue(amount)
      await wrapper.get('form').trigger('submit')
      expect(generate).not.toHaveBeenCalled()
      expect(wrapper.text()).toContain('welfareRedeem.quotaRequired')
      wrapper.unmount()
    }
  )

  it.each(['0', '1.5', '36501'])('rejects invalid extension days %s', async days => {
    const wrapper = await openGenerator()
    await wrapper.get('[data-test="subscription-group"]').setValue('8')
    await wrapper.get('[data-test="subscription-purpose"]').setValue('extend_subscription')
    await wrapper.get('[data-test="supplement-days"]').setValue(days)
    await wrapper.get('form').trigger('submit')
    expect(generate).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('welfareRedeem.daysRequired')
    wrapper.unmount()
  })

  it('retains the ordinary draft after cancelling and reopening the generator', async () => {
    const wrapper = await openGenerator()
    await wrapper.get('[data-test="subscription-group"]').setValue('8')
    await wrapper.get('[data-test="generate-count"]').setValue('3')
    await wrapper.findAll('form button').find(b => b.text() === 'common.cancel')!.trigger('click')
    await wrapper.findAll('button').find(b => b.text() === 'admin.redeem.generateCodes')!.trigger('click')
    expect(wrapper.get('form select').element.value).toBe('subscription')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(generate).toHaveBeenCalledWith(3, 'subscription', 10, 8, 30, undefined)
    wrapper.unmount()
  })

  it('keeps the generated code result if the dialog is closed while the request is pending', async () => {
    let finishGeneration!: (codes: unknown[]) => void
    generate.mockImplementationOnce(() => new Promise(resolve => { finishGeneration = resolve }))
    const wrapper = await openGenerator()
    await wrapper.get('[data-test="subscription-group"]').setValue('8')
    await wrapper.get('form').trigger('submit')
    await wrapper.findAll('form button').find(b => b.text() === 'common.cancel')!.trigger('click')
    finishGeneration([{ id: 9, code: 'FINISHED-CODE' }])
    await flushPromises()
    expect(wrapper.text()).toContain('admin.redeem.generatedSuccessfully')
    expect(wrapper.get('textarea').element.value).toBe('FINISHED-CODE')
    wrapper.unmount()
  })

  it('shows welfare amounts and redemption-time receipts in the admin list', async () => {
    listRedeemCodes.mockResolvedValue({ items: [{ id: 4, code: 'WELFARE', type: 'subscription', value: 0,
      subscription_effect: 'add_quota', quota_amount_usd: 12.5, welfare_campaign_name: 'Autumn invitation',
      welfare_result: { subscription_id: 55, monthly_limit_usd: 112.5, monthly_usage_usd: 20, remaining_usd: 92.5, expires_at: '2027-01-01T00:00:00Z' }
    }], total: 1 })
    const wrapper = await openGenerator()
    expect(wrapper.text()).toContain('+$12.50')
    expect(wrapper.text()).toContain('Autumn invitation')
    expect(wrapper.text()).toContain('welfareRedeem.resultAtRedemption')
    expect(wrapper.text()).toContain('$92.50')
    wrapper.unmount()
  })
})
