import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminGroup } from "@/types";
import GroupsView from "@/views/admin/GroupsView.vue";

const {
  listGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  createGroup,
  updateGroup,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
}));

vi.mock("@/api/admin", () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: vi.fn(),
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: createGroup,
      update: updateGroup,
      delete: vi.fn(),
      duplicate: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn(),
    },
  },
}));

vi.mock("@/api/admin/channels", () => ({
  default: { getModelDefaultPricing: vi.fn().mockResolvedValue({ found: false }) },
}));

vi.mock("@/stores/app", () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}));

vi.mock("@/stores/onboarding", () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

const sourceGroup = {
  id: 42,
  name: "OpenAI",
  description: null,
  platform: "openai",
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: "active",
  subscription_type: "standard",
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  long_context_pricing_enabled: true,
  force_openai_fast: false,
  force_openai_ultrafast: false,
  free_openai_fast: false,
  model_pricing: [],
  profit_control_enabled: false,
  profit_min_margin: 0,
  profit_safety_buffer: 0,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  search_price_per_1k: null,
  audio_realtime_price_per_min: null,
  audio_tts_price_per_million_chars: null,
  audio_stt_price_per_hour: null,
  peak_rate_enabled: false,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  allow_live: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: "2026-09-05T00:00:00Z",
  updated_at: "2026-09-05T00:00:00Z",
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  models_list_config: undefined,
  codex_models_manifest_config: {
    enabled: false,
    account_ids: [],
    fallback_to_scheduler: false,
  },
  sort_order: 10,
} satisfies AdminGroup;

const AppLayoutStub = defineComponent({
  template: "<main><slot /></main>",
});

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
});

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
  },
  template: '<div><div v-if="data.length"><slot name="cell-actions" :row="data[0]" /></div></div>',
});

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
});

// 表单提交前会调用这些子组件的 validate()，自动桩不提供该方法。
const ValidatingFieldStub = defineComponent({
  setup(_, { expose }) {
    expose({ validate: () => true, resetValidation: () => undefined });
    return () => h("div");
  },
});

const mountView = () =>
  mount(GroupsView, {
    global: {
      plugins: [createPinia()],
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        ReasoningEffortPolicyFields: ValidatingFieldStub,
        CodexManifestAccountsField: ValidatingFieldStub,
        ModelTagInput: true,
        VueDraggable: true,
      },
    },
  });

const openEditDialog = async (group: AdminGroup) => {
  listGroups.mockResolvedValue({
    items: [group],
    total: 1,
    page: 1,
    page_size: 20,
    pages: 1,
  });
  const wrapper = mountView();
  await flushPromises();
  const editButton = wrapper
    .findAll("button")
    .find((button) => button.text().includes("common.edit"));
  expect(editButton).toBeTruthy();
  await editButton!.trigger("click");
  await flushPromises();
  return wrapper;
};

const toggle = (
  wrapper: ReturnType<typeof mountView>,
  name: "fast" | "ultrafast",
) => wrapper.get(`[data-testid="edit-force-openai-${name}"]`);

const isOn = (wrapper: ReturnType<typeof mountView>, name: "fast" | "ultrafast") =>
  toggle(wrapper, name).attributes("aria-checked") === "true";

const submit = async (wrapper: ReturnType<typeof mountView>) => {
  await wrapper.get("#edit-group-form").trigger("submit");
  await flushPromises();
  expect(updateGroup).toHaveBeenCalledTimes(1);
  expect(updateGroup.mock.calls[0][0]).toBe(sourceGroup.id);
  return updateGroup.mock.calls[0][1];
};

describe("GroupsView OpenAI forced service tier", () => {
  beforeEach(() => {
    localStorage.clear();
    listGroups.mockReset();
    getModelAllowlistCandidates.mockReset();
    getUsageSummary.mockReset();
    getCapacitySummary.mockReset();
    getLiveCapability.mockReset();
    createGroup.mockReset();
    updateGroup.mockReset();

    getModelAllowlistCandidates.mockResolvedValue([]);
    getUsageSummary.mockResolvedValue([]);
    getCapacitySummary.mockResolvedValue([]);
    getLiveCapability.mockResolvedValue({ supported: false });
    updateGroup.mockResolvedValue({});
  });

  it("shows both switches off for a group that follows the client", async () => {
    const wrapper = await openEditDialog(sourceGroup);

    expect(isOn(wrapper, "fast")).toBe(false);
    expect(isOn(wrapper, "ultrafast")).toBe(false);

    wrapper.unmount();
  });

  it.each([
    [{ force_openai_fast: true, force_openai_ultrafast: false }],
    [{ force_openai_fast: false, force_openai_ultrafast: true }],
  ])("loads and resaves the stored flags %o unchanged", async (flags) => {
    const wrapper = await openEditDialog({ ...sourceGroup, ...flags });

    expect(isOn(wrapper, "fast")).toBe(flags.force_openai_fast);
    expect(isOn(wrapper, "ultrafast")).toBe(flags.force_openai_ultrafast);
    expect(await submit(wrapper)).toMatchObject(flags);

    wrapper.unmount();
  });

  it.each(["fast", "ultrafast"] as const)("turning on %s clears the other forced tier", async (tier) => {
    const wrapper = await openEditDialog({
      ...sourceGroup,
      force_openai_fast: tier === "ultrafast",
      force_openai_ultrafast: tier === "fast",
    });
    await toggle(wrapper, tier).trigger("click");
    expect(await submit(wrapper)).toMatchObject({
      force_openai_fast: tier === "fast",
      force_openai_ultrafast: tier === "ultrafast",
    });
    wrapper.unmount();
  });

  it("preserves model pricing tier overrides when saving unrelated group settings", async () => {
    const pricing = {
      platform: "openai",
      models: ["model-with-overrides"],
      billing_mode: "token" as const,
      input_price: null,
      output_price: null,
      cache_write_price: null,
      cache_read_price: null,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      fast_multiplier: 2,
      flex_multiplier: 0.5,
      ultrafast_multiplier: 4,
      intervals: [],
      time_pricing: null,
    };
    const wrapper = await openEditDialog({ ...sourceGroup, model_pricing: [pricing] });
    expect(wrapper.getComponent({ name: "PricingEntryCard" }).props("enableTierMultipliers")).toBe(true);
    expect(await submit(wrapper)).toMatchObject({ model_pricing: [pricing] });
    wrapper.unmount();
  });

  it("rejects invalid group tier rates and saves edited or cleared rates", async () => {
    const wrapper = await openEditDialog({ ...sourceGroup, model_pricing: [] });
    const add = wrapper.findAll("button").find(button => button.text().includes("admin.groups.modelPricing.add"))!;
    await add.trigger("click");
    const card = wrapper.getComponent({ name: "PricingEntryCard" });
    card.getComponent({ name: "ModelTagInput" }).vm.$emit("update:models", ["gpt-6.2-sol"]);
    await flushPromises();
    const input = (label: string) => card.findAll("label")
      .find(el => el.text() === `admin.channels.form.${label}Multiplier`)!
      .element.parentElement!.querySelector("input")!;
    const setRate = async (label: string, value: string) => {
      const field = input(label);
      field.value = value;
      field.dispatchEvent(new Event("input"));
      await flushPromises();
    };
    await setRate("fast", "3");
    await setRate("flex", "0.6");
    await setRate("ultrafast", "0");
    await wrapper.get("#edit-group-form").trigger("submit");
    expect(updateGroup).not.toHaveBeenCalled();
    await setRate("ultrafast", "5");
    await setRate("flex", "");
    expect(await submit(wrapper)).toMatchObject({
      model_pricing: [{ fast_multiplier: 3, flex_multiplier: null, ultrafast_multiplier: 5 }],
    });
    wrapper.unmount();
  });

  it("normalizes stored overlapping flags and allows disabling Ultrafast", async () => {
    const wrapper = await openEditDialog({
      ...sourceGroup,
      force_openai_fast: true,
      force_openai_ultrafast: true,
    });
    expect(isOn(wrapper, "fast")).toBe(false);
    expect(isOn(wrapper, "ultrafast")).toBe(true);
    await toggle(wrapper, "ultrafast").trigger("click");
    expect(await submit(wrapper)).toMatchObject({
      force_openai_fast: false,
      force_openai_ultrafast: false,
    });
    wrapper.unmount();
  });
});
