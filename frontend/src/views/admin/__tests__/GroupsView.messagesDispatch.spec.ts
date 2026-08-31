import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";

import type { AdminGroup } from "@/types";
import GroupsView from "../GroupsView.vue";
import { defaultMessagesDispatchExactMappings } from "../groupsMessagesDispatch";

const {
  listGroups,
  getAllGroups,
  createGroupApi,
  getModelsListCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  listAccounts,
  showError,
  showSuccess,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getAllGroups: vi.fn(),
  createGroupApi: vi.fn(),
  getModelsListCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  listAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}));

vi.mock("@/api/admin", () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: getAllGroups,
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: createGroupApi,
      update: vi.fn(),
      delete: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: listAccounts,
    },
  },
}));

vi.mock("@/stores/app", () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}));

vi.mock("@/stores/onboarding", () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  };
});

const listedGroup: AdminGroup = {
  id: 1,
  name: "Core Anthropic",
  description: null,
  platform: "anthropic",
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: "active",
  subscription_type: "standard",
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: "",
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 3,
  active_account_count: 2,
  rate_limited_account_count: 1,
  models_list_config: undefined,
  sort_order: 10,
};

const AppLayoutStub = { template: "<div><slot /></div>" };
const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
};
const DataTableStub = {
  props: ["columns", "data"],
  template: "<div></div>",
};
const SelectStub = {
  props: ["modelValue", "options", "placeholder"],
  emits: ["update:modelValue", "change"],
  template: `
    <select
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value); $emit('change')"
    >
      <option v-for="option in options" :key="String(option.value)" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `,
};
const BaseDialogStub = {
  props: ["show"],
  template: "<div v-if=\"show\"><slot /><slot name=\"footer\" /></div>",
};

const mountView = async () => {
  const wrapper = mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        ReasoningEffortPolicyFields: {
          template: "<div />",
          setup(_, { expose }: { expose: (api: { validate: () => boolean; resetValidation: () => void }) => void }) {
            expose({
              validate: () => true,
              resetValidation: () => {},
            });
            return {};
          },
        },
        VueDraggable: { template: "<div><slot /></div>" },
      },
    },
  });
  await flushPromises();
  return wrapper;
};

describe("admin GroupsView messages dispatch defaults", () => {
  beforeEach(() => {
    localStorage.clear();
    for (const fn of [
      listGroups,
      getAllGroups,
      createGroupApi,
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      listAccounts,
      showError,
      showSuccess,
      isCurrentStep,
      nextStep,
    ]) {
      fn.mockReset();
    }

    listGroups.mockResolvedValue({
      items: [listedGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    });
    getAllGroups.mockResolvedValue([]);
    getModelsListCandidates.mockResolvedValue([]);
    getUsageSummary.mockResolvedValue([]);
    getCapacitySummary.mockResolvedValue([]);
    getLiveCapability.mockResolvedValue({ supported: false });
    listAccounts.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0,
    });
    createGroupApi.mockResolvedValue({ ...listedGroup, id: 99, platform: "openai" });
    isCurrentStep.mockReturnValue(false);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("submits 1M Claude exact mappings when creating an OpenAI group from the anthropic default form", async () => {
    const wrapper = await mountView();

    await wrapper.get('[data-tour="groups-create-btn"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-tour="group-form-name"]').setValue("OpenAI Dispatch");
    await wrapper
      .get('[data-tour="group-form-platform"]')
      .setValue("openai");
    await flushPromises();

    await wrapper.get("#create-group-form").trigger("submit");
    await flushPromises();

    expect(createGroupApi).toHaveBeenCalledTimes(1);
    const payload = createGroupApi.mock.calls[0][0] as {
      messages_dispatch_model_config?: {
        opus_mapped_model?: string;
        sonnet_mapped_model?: string;
        haiku_mapped_model?: string;
        exact_model_mappings?: Record<string, string>;
      };
    };
    expect(payload.messages_dispatch_model_config).toMatchObject({
      opus_mapped_model: "gpt-5.6-sol",
      sonnet_mapped_model: "gpt-5.6-sol",
      haiku_mapped_model: "gpt-5.6-luna",
      exact_model_mappings: Object.fromEntries(
        defaultMessagesDispatchExactMappings().map((row) => [
          row.claude_model,
          row.target_model,
        ]),
      ),
    });
    expect(
      payload.messages_dispatch_model_config?.exact_model_mappings?.[
        "claude-fable-5"
      ],
    ).toBe("gpt-5.6-sol");
    wrapper.unmount();
  });
});
