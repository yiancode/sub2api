import type { OpenAIMessagesDispatchModelConfig } from "@/types";

export interface MessagesDispatchMappingRow {
  claude_model: string;
  target_model: string;
}

export interface MessagesDispatchFormState {
  allow_messages_dispatch: boolean;
  opus_mapped_model: string;
  sonnet_mapped_model: string;
  haiku_mapped_model: string;
  exact_model_mappings: MessagesDispatchMappingRow[];
}

const defaultSolModel = "gpt-5.6-sol";
const defaultLunaModel = "gpt-5.6-luna";

export function defaultMessagesDispatchExactMappings(): MessagesDispatchMappingRow[] {
  return [
    { claude_model: "claude-fable-5", target_model: defaultSolModel },
    { claude_model: "claude-sonnet-5", target_model: defaultSolModel },
    { claude_model: "claude-opus-5", target_model: defaultSolModel },
    { claude_model: "claude-opus-4-8", target_model: defaultSolModel },
    { claude_model: "claude-opus-4-7", target_model: defaultSolModel },
    { claude_model: "claude-opus-4-6", target_model: defaultSolModel },
    { claude_model: "claude-sonnet-4-6", target_model: defaultSolModel },
    { claude_model: "claude-haiku-4-5", target_model: defaultLunaModel },
  ];
}

export function supportsMessagesDispatchPlatform(platform: string): boolean {
  return platform === "openai" || platform === "all" || platform === "composite";
}

export function createDefaultMessagesDispatchFormState(): MessagesDispatchFormState {
  return {
    allow_messages_dispatch: false,
    opus_mapped_model: defaultSolModel,
    sonnet_mapped_model: defaultSolModel,
    haiku_mapped_model: defaultLunaModel,
    exact_model_mappings: defaultMessagesDispatchExactMappings(),
  };
}

export function messagesDispatchConfigToFormState(
  config?: OpenAIMessagesDispatchModelConfig | null,
): MessagesDispatchFormState {
  const defaults = createDefaultMessagesDispatchFormState();
  const exactMappings = Object.entries(config?.exact_model_mappings || {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([claude_model, target_model]) => ({ claude_model, target_model }));

  return {
    allow_messages_dispatch: false,
    opus_mapped_model:
      config?.opus_mapped_model?.trim() || defaults.opus_mapped_model,
    sonnet_mapped_model:
      config?.sonnet_mapped_model?.trim() || defaults.sonnet_mapped_model,
    haiku_mapped_model:
      config?.haiku_mapped_model?.trim() || defaults.haiku_mapped_model,
    exact_model_mappings: exactMappings,
  };
}

export function messagesDispatchFormStateToConfig(
  state: MessagesDispatchFormState,
): OpenAIMessagesDispatchModelConfig {
  const exactModelMappings = Object.fromEntries(
    state.exact_model_mappings
      .map((row) => [row.claude_model.trim(), row.target_model.trim()] as const)
      .filter(([claudeModel, targetModel]) => claudeModel && targetModel),
  );

  return {
    opus_mapped_model: state.opus_mapped_model.trim(),
    sonnet_mapped_model: state.sonnet_mapped_model.trim(),
    haiku_mapped_model: state.haiku_mapped_model.trim(),
    exact_model_mappings: exactModelMappings,
  };
}

export function resetMessagesDispatchFormState(
  target: MessagesDispatchFormState,
): void {
  const defaults = createDefaultMessagesDispatchFormState();
  target.allow_messages_dispatch = defaults.allow_messages_dispatch;
  target.opus_mapped_model = defaults.opus_mapped_model;
  target.sonnet_mapped_model = defaults.sonnet_mapped_model;
  target.haiku_mapped_model = defaults.haiku_mapped_model;
  target.exact_model_mappings = defaults.exact_model_mappings;
}
