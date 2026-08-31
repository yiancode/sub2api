import { describe, expect, it } from "vitest";

import {
  createDefaultMessagesDispatchFormState,
  messagesDispatchConfigToFormState,
  messagesDispatchFormStateToConfig,
  resetMessagesDispatchFormState,
  supportsMessagesDispatchPlatform,
} from "../groupsMessagesDispatch";

describe("groupsMessagesDispatch", () => {
  it("supports OpenAI and composite groups only", () => {
    expect(supportsMessagesDispatchPlatform("openai")).toBe(true);
    expect(supportsMessagesDispatchPlatform("composite")).toBe(true);
    expect(supportsMessagesDispatchPlatform("all")).toBe(false);
    expect(supportsMessagesDispatchPlatform("anthropic")).toBe(false);
  });

  it("returns Sol/Luna defaults plus 1M Claude exact mappings", () => {
    expect(createDefaultMessagesDispatchFormState()).toEqual({
      allow_messages_dispatch: false,
      opus_mapped_model: "gpt-5.6-sol",
      sonnet_mapped_model: "gpt-5.6-sol",
      haiku_mapped_model: "gpt-5.6-luna",
      exact_model_mappings: [
        { claude_model: "claude-fable-5", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-sonnet-5", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-opus-5", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-opus-4-8", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-opus-4-7", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-opus-4-6", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-sonnet-4-6", target_model: "gpt-5.6-sol" },
        { claude_model: "claude-haiku-4-5", target_model: "gpt-5.6-luna" },
      ],
    });
  });

  it("sanitizes exact model mapping rows when converting to config", () => {
    const config = messagesDispatchFormStateToConfig({
      allow_messages_dispatch: true,
      opus_mapped_model: " gpt-5.4 ",
      sonnet_mapped_model: "gpt-5.3-codex",
      haiku_mapped_model: " gpt-5.4-mini ",
      exact_model_mappings: [
        {
          claude_model: " claude-sonnet-4-5-20250929 ",
          target_model: " gpt-5.2 ",
        },
        { claude_model: "", target_model: "gpt-5.4" },
        { claude_model: "claude-opus-4-6", target_model: " " },
      ],
    });

    expect(config).toEqual({
      opus_mapped_model: "gpt-5.4",
      sonnet_mapped_model: "gpt-5.3-codex",
      haiku_mapped_model: "gpt-5.4-mini",
      exact_model_mappings: {
        "claude-sonnet-4-5-20250929": "gpt-5.2",
      },
    });
  });

  it("hydrates form state from api config", () => {
    expect(
      messagesDispatchConfigToFormState({
        opus_mapped_model: "gpt-5.4",
        sonnet_mapped_model: "gpt-5.2",
        haiku_mapped_model: "gpt-5.4-mini",
        exact_model_mappings: {
          "claude-opus-4-6": "gpt-5.4",
          "claude-haiku-4-5-20251001": "gpt-5.4-mini",
        },
      }),
    ).toEqual({
      allow_messages_dispatch: false,
      opus_mapped_model: "gpt-5.4",
      sonnet_mapped_model: "gpt-5.2",
      haiku_mapped_model: "gpt-5.4-mini",
      exact_model_mappings: [
        {
          claude_model: "claude-haiku-4-5-20251001",
          target_model: "gpt-5.4-mini",
        },
        { claude_model: "claude-opus-4-6", target_model: "gpt-5.4" },
      ],
    });
  });

  it("resets mutable form state when platform switches away from openai", () => {
    const state = {
      allow_messages_dispatch: true,
      opus_mapped_model: "gpt-5.2",
      sonnet_mapped_model: "gpt-5.4",
      haiku_mapped_model: "gpt-5.1",
      exact_model_mappings: [
        { claude_model: "claude-opus-4-6", target_model: "gpt-5.4" },
      ],
    };

    resetMessagesDispatchFormState(state);

    expect(state).toEqual(createDefaultMessagesDispatchFormState());
  });
});
