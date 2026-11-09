import { describe, expect, it } from "vitest";

import {
  normalizeGroupOpenAIFast,
  normalizeGroupOpenAIForcedTiers,
  supportsGroupOpenAIFast,
  supportsGroupOpenAIUltraFast,
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

  it("only enables UltraFast for OpenAI and resolves conflicting forced tiers", () => {
    expect(supportsGroupOpenAIUltraFast("openai")).toBe(true);
    expect(supportsGroupOpenAIUltraFast("composite")).toBe(false);
    expect(normalizeGroupOpenAIForcedTiers("openai", true, true)).toEqual({
      force_openai_fast: false, force_openai_ultrafast: true,
    });
    expect(normalizeGroupOpenAIForcedTiers("composite", true, true)).toEqual({
      force_openai_fast: true, force_openai_ultrafast: false,
    });
    expect(normalizeGroupOpenAIForcedTiers("anthropic", true, true)).toEqual({
      force_openai_fast: false, force_openai_ultrafast: false,
    });
    expect(normalizeGroupOpenAIForcedTiers("openai")).toEqual({
      force_openai_fast: false, force_openai_ultrafast: false,
    });
    expect(normalizeGroupOpenAIForcedTiers("openai", null, null)).toEqual({
      force_openai_fast: false, force_openai_ultrafast: false,
    });
  });

  it("provides localized switch labels", () => {
    expect(zh.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
    expect(en.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
    for (const locale of [zh, en]) {
      expect(locale.groups.openaiUltraFast).toMatchObject({
        force: expect.stringContaining("UltraFast"),
        hint: expect.stringContaining("service_tier=ultrafast"),
      });
    }
  });
});
