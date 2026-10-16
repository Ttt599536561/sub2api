import { describe, expect, it } from "vitest";

import {
  normalizeGroupOpenAIFast,
  normalizeGroupOpenAIUltrafast,
  setGroupOpenAIFast,
  setGroupOpenAIUltrafast,
  supportsGroupOpenAIFast,
  supportsGroupOpenAIUltrafast,
} from "../groupsOpenAIFast";
import en from "@/i18n/locales/en/admin/overview";
import zh from "@/i18n/locales/zh/admin/overview";

describe("groupsOpenAIFast", () => {
  it("supports OpenAI and composite groups", () => {
    expect(supportsGroupOpenAIFast("openai")).toBe(true);
    expect(supportsGroupOpenAIFast("composite")).toBe(true);
    expect(supportsGroupOpenAIFast("anthropic")).toBe(false);
  });

  it("clears stale enabled state on unsupported platforms", () => {
    expect(normalizeGroupOpenAIFast("openai", true)).toBe(true);
    expect(normalizeGroupOpenAIFast("composite", true)).toBe(true);
    expect(normalizeGroupOpenAIFast("anthropic", true)).toBe(false);
    expect(normalizeGroupOpenAIFast("openai", false)).toBe(false);
  });

  it("supports Ultrafast only for OpenAI groups", () => {
    expect(supportsGroupOpenAIUltrafast("openai")).toBe(true);
    expect(supportsGroupOpenAIUltrafast("composite")).toBe(false);
    expect(supportsGroupOpenAIUltrafast("anthropic")).toBe(false);
    expect(normalizeGroupOpenAIUltrafast("openai", true)).toBe(true);
    expect(normalizeGroupOpenAIUltrafast("composite", true)).toBe(false);
  });

  it("keeps forced Fast and Ultrafast mutually exclusive", () => {
    const state = { force_openai_fast: false, force_openai_ultrafast: false };
    setGroupOpenAIUltrafast(state, true);
    expect(state).toEqual({ force_openai_fast: false, force_openai_ultrafast: true });
    setGroupOpenAIFast(state, true);
    expect(state).toEqual({ force_openai_fast: true, force_openai_ultrafast: false });
    setGroupOpenAIFast(state, false);
    expect(state).toEqual({ force_openai_fast: false, force_openai_ultrafast: false });
  });

  it("provides localized switch labels", () => {
    expect(zh.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      ultrafast: expect.any(String),
      ultrafastHint: expect.stringContaining("8"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
    expect(en.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      ultrafast: expect.any(String),
      ultrafastHint: expect.stringContaining("8"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
  });
});
