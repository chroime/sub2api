# Model Pricing Data

This directory contains a local copy of the mirrored model pricing data as a fallback mechanism.

## Source
The original file is maintained by the LiteLLM project and mirrored into the `price-mirror` branch of this repository via GitHub Actions:
- Mirror branch (configurable via `PRICE_MIRROR_REPO`): https://raw.githubusercontent.com/<your-repo>/price-mirror/model_prices_and_context_window.json
- Upstream source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. First attempt to download the latest version from GitHub
2. If download fails, use this local copy as fallback
3. Log a warning when using the fallback file

## Manual Update
When refreshing the upstream snapshot, merge upstream USD entries while preserving
the curated domestic CNY cards and their provenance. Replacing this entire file
with the upstream download would discard those local prices. Refresh the native
CNY reference entries with the importer described below.

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Last updated: 2025-08-10

## Domestic CNY prices (2026-11-23)

The same `model_prices_and_context_window.json` now includes 55 domestic CNY
model entries. Domestic prices are numeric CNY per token (`currency: "CNY"`);
other entries keep their existing USD units. Billing and the model plaza use
the same parsed cards. Curated domestic CNY cards take precedence over remote
USD quotes; `pricing.override_file` and group/channel prices remain higher
priority. Remote downloading and hash verification are unchanged.

This update includes 30 additional entries and corrected DeepSeek list quotes from
[MofCloud's LLM Explorer](https://mofcloud.cn/tools/llm-explorer/), which labels
46 models with native CNY prices. Exact original-currency amounts and all input
context bands come from the referenced dataset:
[LLM Abacus](https://www.llmabacus.com),
[public JSON](https://github.com/szp2005/llm-prices-cn/blob/main/prices.json).
Pricing data provided by LLM Abacus is used and adapted under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
Each imported entry records its vendor source URL, reference model ID, license,
source verification date, and collection date. Collection does not imply the
vendor pricing page was reverified on that day.

The 25 directly verified vendor entries from the previous local override are
also included in this shipped catalog. They are preserved when the reference
is imported again, except DeepSeek, which now uses the site's exact **peak/list**
CNY quote: Pro ¥9/¥27/¥0.30 and Flash ¥2/¥8/¥0.04 per million tokens
(input/output/cache read). These cards set `deepseek_peak_price: true`;
billing applies 0.5x during off-peak hours and 1x during peak hours, keeping
the official reference quote intact without charging the peak twice.
Older off-peak cards retain their existing 1x/2x time policy. Operator
override fields remain final rates and do not receive either adjustment.
MiMo's ¥0.025/MTok
cache price retains its full precision. Missing cache quotes stay absent.

The user-confirmed Qwen3.8 reference image sets Flash to CNY ¥0.79 input,
¥2.67 output, ¥0.10 cache read, and Max to ¥12 input, ¥36 output, ¥0.22 cache
read per million tokens. These explicit reference quotes are preserved by the
importer. The image does not quote cache-write prices, so those fields stay absent.

USD-converted domestic display quotes, reseller prices, retired models, and
unverified aliases (for example `kimi-k2.7` versus `kimi-k2.7-code`) are excluded.
GLM-4.7 is also excluded: the vendor's short-context prices change when output
reaches 200 tokens, which the reference dataset's input-only bands omit.
The website's slugs are explicitly mapped to catalog IDs in the import script;
adding a price does not configure an upstream account or advertise model
availability. Context tiers use `(min_tokens, max_tokens]` bands, with the
whole request charged at the matching band's prices.

To refresh these reference entries manually:

```sh
python backend/scripts/import_domestic_pricing.py
```

For a reproducible offline import, use `--index` with a saved MofCloud HTML page
and `--dataset` with the saved LLM Abacus JSON. The script rejects unreviewed
model mappings, non-CNY original currencies, and invalid prices. It does not
create a second runtime pricing catalog or schedule automatic web scraping.
