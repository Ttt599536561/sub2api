export function supportsGroupOpenAIFast(platform: string): boolean {
  return platform === "openai" || platform === "composite";
}

export function normalizeGroupOpenAIFast(
  platform: string,
  enabled: boolean,
): boolean {
  return supportsGroupOpenAIFast(platform) && enabled;
}

export function supportsGroupOpenAIUltrafast(platform: string): boolean {
  return platform === "openai";
}

export function normalizeGroupOpenAIUltrafast(
  platform: string,
  enabled: boolean,
): boolean {
  return supportsGroupOpenAIUltrafast(platform) && enabled;
}

export type OpenAIFastToggleState = {
  force_openai_fast: boolean;
  force_openai_ultrafast: boolean;
};

export function normalizeGroupOpenAIUltrafastState(
  platform: string,
  state: OpenAIFastToggleState,
): void {
  if (!supportsGroupOpenAIUltrafast(platform)) {
    state.force_openai_ultrafast = false;
  }
  if (state.force_openai_ultrafast) {
    state.force_openai_fast = false;
  }
}

export function setGroupOpenAIFast(
  state: OpenAIFastToggleState,
  enabled: boolean,
): void {
  state.force_openai_fast = enabled;
  if (enabled) {
    state.force_openai_ultrafast = false;
  }
}

export function setGroupOpenAIUltrafast(
  state: OpenAIFastToggleState,
  enabled: boolean,
): void {
  state.force_openai_ultrafast = enabled;
  if (enabled) {
    state.force_openai_fast = false;
  }
}
