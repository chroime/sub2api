#!/usr/bin/env python3
"""Import native CNY reference prices into the existing shared pricing catalog.

Reference: https://mofcloud.cn/tools/llm-explorer/
Exact original-currency prices: LLM Abacus, https://www.llmabacus.com (CC-BY-4.0).
USD-derived display prices and reseller quotes are deliberately excluded.
"""

import argparse
from datetime import date
from decimal import Decimal
from html.parser import HTMLParser
import json
from pathlib import Path
from urllib.request import Request, urlopen


REFERENCE = "https://mofcloud.cn/tools/llm-explorer/"
DATASET = "https://raw.githubusercontent.com/szp2005/llm-prices-cn/main/prices.json"
CATALOG = Path(__file__).resolve().parents[1] / "resources/model-pricing/model_prices_and_context_window.json"

EXCLUDED_CONDITIONAL_MODELS = {
    "glm-4-7": "Official short-context prices also depend on output length (<200 / >=200 tokens); the reference's input-only bands omit that condition.",
}

# Website slugs are not API model IDs. Keep the mapping explicit and reviewable.
MODEL_IDS = {
    "glm-5-3-flashx": "glm-5.3-flashx",
    "glm-5-3-flash": "glm-5.3-flash",
    "glm-5-3": "glm-5.3",
    "deepseek-v4-pro": "deepseek-v4-pro",
    "qwen3-8-max": "qwen3.8-max",
    "kimi-k3": "kimi-k3",
    "glm-5-2": "glm-5.2",
    "kimi-k2-7-code": "kimi-k2.7-code",
    "kimi-k2-7-code-highspeed": "kimi-k2.7-code-highspeed",
    "minimax-m3": "minimax-m3",
    "qwen3-7-max": "qwen3.7-max",
    "kimi-k2-6": "kimi-k2.6",
    "glm-5-1": "glm-5.1",
    "minimax-m2-7": "minimax-m2.7",
    "qwen3-5-flash": "qwen3.5-flash",
    "qwen3-5-plus": "qwen3.5-plus",
    "glm-5": "glm-5",
    "glm-4-7": "glm-4.7",
    "baichuan-m2": "baichuan-m2",
    "baichuan-m3-plus": "baichuan-m3-plus",
    "deepseek-v4-flash": "deepseek-v4-flash",
    "doubao-1-5-pro": "doubao-1.5-pro",
    "doubao-1-6": "doubao-1.6",
    "doubao-seed-2-1-pro": "doubao-seed-2.1-pro",
    "mimo-v2-5": "mimo-v2.5",
    "mimo-v2-6-flash": "mimo-v2.6-flash",
    "mimo-v2-6-pro": "mimo-v2.6-pro",
    "mimo-v2-6-pro-ultraspeed": "mimo-v2.6-pro-ultraspeed",
    "minimax-m2-7-highspeed": "minimax-m2.7-highspeed",
    "qwen3-max": "qwen3-max",
    "qwen3-6-max-preview": "qwen3.6-max-preview",
    "qwen3-8-max-prime": "qwen3.8-max-prime",
    "spark-pro": "spark-pro",
    "spark-ultra": "spark-ultra",
    "spark-x2": "spark-x2",
    "spark-x2-flash": "spark-x2-flash",
    "spark-x2-5": "spark-x2.5",
    "step-3-7-flash": "step-3.7-flash",
    "ernie-4-5-turbo": "ernie-4.5-turbo",
    "ernie-5-1": "ernie-5.1",
    "ernie-x1-turbo": "ernie-x1-turbo",
    "hunyuan-2-0-think": "hunyuan-2.0-think",
    "hunyuan-hy3": "hy3",
    "hunyuan-hy4-preview": "hy4-preview",
    "hunyuan-t1": "hunyuan-t1",
    "hunyuan-turbos": "hunyuan-turbos",
}


class NativeCNYModels(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids = set()
        self.is_native_row = False

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "tr":
            self.is_native_row = attrs.get("data-origin") == "cn" and attrs.get("data-currency") == "CNY"
        elif tag == "a" and self.is_native_row:
            href = attrs.get("href", "")
            if href.startswith("/tools/llm-explorer/"):
                self.ids.add(href.rstrip("/").rsplit("/", 1)[-1])

    def handle_endtag(self, tag):
        if tag == "tr":
            self.is_native_row = False


def per_token(value):
    price = Decimal(str(value))
    if not price.is_finite() or price < 0:
        raise ValueError("Price must be a finite, nonnegative native CNY value")
    return float(price / Decimal(1_000_000))


def price_fields(model):
    fields = {
        "input_cost_per_token": per_token(model["input_price_orig_per_m"]),
        "output_cost_per_token": per_token(model["output_price_orig_per_m"]),
    }
    cached = model.get("cached_input_price_orig_per_m")
    if cached is not None:
        fields["cache_read_input_token_cost"] = per_token(cached)
    tiers = model.get("price_tiers", [])
    if tiers:
        intervals = []
        minimum = 0
        for tier in tiers:
            maximum = tier.get("up_to_input_tokens")
            if maximum is not None and maximum <= minimum:
                raise ValueError("Context pricing bounds must increase")
            interval = {
                "min_tokens": minimum,
                "input_price": per_token(tier["input_price_orig_per_m"]),
                "output_price": per_token(tier["output_price_orig_per_m"]),
            }
            if maximum is not None:
                interval["max_tokens"] = maximum
            cached = tier.get("cached_input_price_orig_per_m")
            if cached is not None:
                interval["cache_read_price"] = per_token(cached)
            intervals.append(interval)
            if maximum is None:
                break
            minimum = maximum
        if intervals[-1].get("max_tokens") is not None:
            raise ValueError("Final context tier must be unbounded")
        fields["context_pricing_tiers"] = intervals
    return fields


def import_models(catalog, index_html, dataset):
    parser = NativeCNYModels()
    parser.feed(index_html)
    if not parser.ids:
        raise ValueError("Reference page has no native CNY model rows")
    models = {model["id"]: model for model in dataset["models"]}
    added, preserved, excluded = [], [], []
    for source_id in sorted(parser.ids):
        if source_id not in MODEL_IDS:
            raise ValueError(f"Review the unmapped reference model ID: {source_id}")
        model_id = MODEL_IDS[source_id]
        if source_id in EXCLUDED_CONDITIONAL_MODELS:
            if catalog.get(model_id, {}).get("pricing_reference") == REFERENCE:
                del catalog[model_id]
            excluded.append({"model": model_id, "reason": EXCLUDED_CONDITIONAL_MODELS[source_id]})
            continue
        model = models[source_id]
        if model.get("billing_currency") != "CNY" or model.get("country") != "CN" or model.get("retired"):
            raise ValueError(f"Reference source is not an active native CNY model: {source_id}")
        existing = catalog.get(model_id, {})
        if existing.get("currency") == "CNY" and existing.get("official_api_verification_status") == "verified":
            preserved.append(model_id)
            continue
        # DeepSeek's reference quote is the peak/list price. Keep that exact
        # quote in the catalog; billing applies the off-peak adjustment separately.
        deepseek = model["vendor_id"] == "deepseek"
        if existing.get("currency") == "CNY" and existing.get("pricing_reference") != REFERENCE and not deepseek:
            preserved.append(model_id)
            continue
        entry = {key: value for key, value in existing.items()
                 if "cost" not in key and not key.startswith("long_context_")
                 and key not in ("official_currency", "context_pricing_tiers")}
        entry.update(price_fields(model))
        if deepseek:
            entry["deepseek_peak_price"] = True
            entry["pricing_notes"] = "Native CNY reference quote (peak/list price). Billing applies the off-peak discount separately."
        entry.update({
            "currency": "CNY",
            "mode": "chat",
            "litellm_provider": model["vendor_id"],
            "source": model["source"],
            "pricing_reference": REFERENCE,
            "pricing_reference_model": source_id,
            "pricing_data_source": "https://www.llmabacus.com",
            "pricing_data_license": "CC-BY-4.0",
            "pricing_verified_at": model.get("last_verified"),
            "pricing_collected_at": date.today().isoformat(),
        })
        catalog[model_id] = entry
        added.append(model_id)
    return {"imported": added, "preserved_direct_vendor_prices": preserved, "excluded_conditional_prices": excluded}


def read_source(path, url):
    if path:
        return Path(path).read_text(encoding="utf-8-sig")
    request = Request(url, headers={"User-Agent": "Sub2API-CNY-Pricing-Import/1.0"})
    with urlopen(request, timeout=30) as response:
        return response.read().decode("utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--index", help="Saved MofCloud HTML; default: download reference page")
    parser.add_argument("--dataset", help="Saved LLM Abacus JSON; default: download public dataset")
    parser.add_argument("--catalog", type=Path, default=CATALOG)
    args = parser.parse_args()
    catalog = json.loads(args.catalog.read_text(encoding="utf-8-sig"))
    result = import_models(catalog, read_source(args.index, REFERENCE), json.loads(read_source(args.dataset, DATASET)))
    args.catalog.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
