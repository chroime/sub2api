export function supportsGroupOpenAIFast(platform: string): boolean {
  return platform === "openai" || platform === "composite";
}

export function normalizeGroupOpenAIFast(
  platform: string,
  enabled: boolean,
): boolean {
  return supportsGroupOpenAIFast(platform) && enabled;
}

export function supportsGroupOpenAIUltraFast(platform: string): boolean {
  return platform === "openai";
}

type ForcedOpenAITiers = {
  force_openai_fast: boolean;
  force_openai_ultrafast: boolean;
};

export function normalizeGroupOpenAIForcedTiers(
  platform: string,
  fast: boolean | null | undefined = false,
  ultrafast: boolean | null | undefined = false,
): ForcedOpenAITiers {
  const forceUltraFast = supportsGroupOpenAIUltraFast(platform) && ultrafast === true;
  return {
    force_openai_fast: normalizeGroupOpenAIFast(platform, fast === true) && !forceUltraFast,
    force_openai_ultrafast: forceUltraFast,
  };
}

export function setGroupOpenAIForcedTier(
  form: ForcedOpenAITiers,
  tier: "fast" | "ultrafast",
  enabled: boolean,
): void {
  if (tier === "ultrafast") {
    form.force_openai_ultrafast = enabled;
    if (enabled) form.force_openai_fast = false;
  } else {
    form.force_openai_fast = enabled;
    if (enabled) form.force_openai_ultrafast = false;
  }
}
