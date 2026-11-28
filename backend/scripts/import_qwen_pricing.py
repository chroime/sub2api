#!/usr/bin/env python3
"""Import Qwen's undiscounted official API token prices into the shared catalog.

The public pricing page uses ListModelSeries. Price is the original amount;
Discount is applied separately by its UI and must never modify this catalog.
"""

import argparse
from datetime import date
from decimal import Decimal, InvalidOperation
import json
from pathlib import Path
import re
from urllib.parse import urlencode
from urllib.request import Request, urlopen


REFERENCE = "https://platform.qianwenai.com/pricing/api"
ENDPOINT = "https://platform-home.qianwenai.com/data/api.json?product=AliyunDeliveryService&action=ListModelSeries"
CATALOG = Path(__file__).resolve().parents[1] / "resources/model-pricing/model_prices_and_context_window.json"
FIELDS = {
    "input_token": "input_cost_per_token",
    "output_token": "output_cost_per_token",
    "input_token_cache_creation_5m": "cache_creation_input_token_cost",
    "input_token_cache_creation_1h": "cache_creation_input_token_cost_above_1hr",
}
INTERVAL_FIELDS = {
    "input_cost_per_token": "input_price",
    "output_cost_per_token": "output_price",
    "cache_read_input_token_cost": "cache_read_price",
    "cache_creation_input_token_cost": "cache_write_price",
    "cache_creation_input_token_cost_above_1hr": "cache_write_1h_price",
}
PRICE_TYPES = set(FIELDS) | {"input_token_cache", "input_token_cache_read"}
RANGE = re.compile(r"(?:(\d+(?:\.\d+)?[km]?)<)?输入<=(\d+(?:\.\d+)?[km]?)", re.I)


def amount(row):
    if row.get("PriceUnit") != "每百万tokens":
        raise ValueError("Unexpected Qwen token price unit")
    try:
        value = Decimal(str(row["Price"]))
    except (InvalidOperation, KeyError):
        raise ValueError("Missing or invalid Qwen original price") from None
    if not value.is_finite() or value < 0:
        raise ValueError("Qwen original prices must be finite and nonnegative")
    return value


def standard_prices(rows):
    selected = {}
    for row in rows:
        kind = row.get("Type", "")
        if row.get("TimeBand") not in (None, "", "standard"):
            continue
        if kind not in PRICE_TYPES and not kind.startswith("thinking_"):
            continue
        if kind in selected and amount(selected[kind]) != amount(row):
            raise ValueError("Conflicting Qwen standard prices")
        selected[kind] = row
    return selected


def fields_from_prices(prices):
    fields = {field: float(amount(prices[kind]) / Decimal(1_000_000))
              for kind, field in FIELDS.items() if kind in prices}
    # This is a complete official price card. No cache-creation line means
    # there is no separately quoted write charge, rather than an unknown price.
    fields.setdefault("cache_creation_input_token_cost", 0.0)
    cache_kind = "input_token_cache" if "input_token_cache" in prices else "input_token_cache_read"
    if cache_kind in prices:
        fields["cache_read_input_token_cost"] = float(amount(prices[cache_kind]) / Decimal(1_000_000))
    return fields, "ordinary" if cache_kind == "input_token_cache" else "explicit" if cache_kind in prices else "not_quoted"


def token_bound(value):
    suffix = value[-1].lower()
    multiplier = {"k": 1000, "m": 1_000_000}.get(suffix, 1)
    number = Decimal(value[:-1] if multiplier != 1 else value) * multiplier
    if number != int(number):
        raise ValueError("Nonintegral Qwen context boundary")
    return int(number)


def price_card(series, item, collected_at):
    common = standard_prices(series.get("Prices", []))
    common.update(standard_prices(item.get("Prices", [])))
    tiers = item.get("MultiPrices") or series.get("MultiPrices") or []
    tiers = [tier for tier in tiers if tier.get("RangeName")]
    bands, original_rows = [], []
    minimum = 0
    for tier in tiers or [{"Prices": []}]:
        prices = dict(common)
        prices.update(standard_prices(tier.get("Prices", [])))
        if "input_token" not in prices or "output_token" not in prices:
            return None, "No unambiguous regular input/output token prices in the official API response."
        for kind in ("input_token", "output_token"):
            thinking = prices.get("thinking_" + kind)
            if thinking is not None and amount(thinking) != amount(prices[kind]):
                return None, "Thinking and nonthinking prices differ; a single catalog card cannot represent both."
        fields, cache_kind = fields_from_prices(prices)
        label = tier.get("RangeName", "")
        maximum = None
        if label:
            match = RANGE.fullmatch(re.sub(r"\s+", "", label).replace("≤", "<="))
            if not match:
                raise ValueError("Unrecognized Qwen context price range: " + label)
            lower, upper = match.groups()
            lower = token_bound(lower) if lower else 0
            maximum = token_bound(upper)
            if lower != minimum or maximum <= lower:
                raise ValueError("Qwen context price ranges must be contiguous and increasing")
        bands.append((minimum, maximum, fields, cache_kind))
        original_rows.append({"range": label, "prices": [row for kind,row in prices.items() if kind in PRICE_TYPES]})
        minimum = maximum
    base = bands[0][2]
    entry = {
        **base, "currency": "CNY", "mode": "chat", "litellm_provider": "dashscope",
        "source": REFERENCE, "pricing_reference": REFERENCE,
        "pricing_data_source": ENDPOINT, "pricing_verified_at": collected_at,
        "pricing_collected_at": collected_at, "pricing_basis": "list_before_discount",
        "official_api_verification_status": "verified", "official_api_checked_at": collected_at,
        "pricing_cache_read_kind": bands[0][3], "official_api_price_rows": original_rows,
        "supports_prompt_caching": any("cache_read_input_token_cost" in band[2] for band in bands),
        "pricing_notes": "Official API regular list prices in native CNY before Discount. Batch, time discounts and tool fees are excluded. Ordinary cache-hit prices take precedence when quoted; distinct explicit-cache prices remain in official_api_price_rows.",
    }
    if tiers:
        intervals = []
        for index, (lower, upper, fields, _) in enumerate(bands):
            interval = {"min_tokens": lower, **{INTERVAL_FIELDS[k]:v for k,v in fields.items()}}
            # Match the existing runtime's unbounded final-tier convention.
            # Keep the official upper bound in source rows and token ceilings.
            if index < len(bands)-1:
                interval["max_tokens"] = upper
            intervals.append(interval)
        entry["context_pricing_tiers"] = intervals
    info = item.get("ModelInfo", {})
    for source, target in (("MaxInputTokens", "max_input_tokens"), ("MaxOutputTokens", "max_output_tokens")):
        value = info.get(source, item.get(source))
        if isinstance(value, int) and not isinstance(value, bool) and value > 0:
            entry[target] = value
    return entry, None


def import_models(catalog, dataset, collected_at=None):
    if str(dataset.get("code")) != "200" or not isinstance(dataset.get("data", {}).get("Data"), list):
        raise ValueError("Official Qwen API response is not successful model-series data")
    data = dataset["data"]
    total = data.get("Ext", {}).get("totalCount")
    if total is not None and (not isinstance(total, int) or total != len(data["Data"])):
        raise ValueError("Official Qwen model-series response is incomplete; collect every page first")
    collected_at = collected_at or date.today().isoformat()
    changes, seen, unsupported = {}, set(), []
    for series in dataset["data"]["Data"]:
        for item in series.get("Items") or [series]:
            model_id = item.get("Model", "")
            if not re.fullmatch(r"(?:qwen|qwq)[a-z0-9._-]*", model_id):
                continue
            seen.add(model_id)
            entry, reason = price_card(series, item, collected_at)
            if entry is None:
                unsupported.append({"model":model_id, "reason":reason})
                continue
            if model_id in changes and changes[model_id] != entry:
                raise ValueError("Conflicting official Qwen model records: " + model_id)
            changes[model_id] = entry
    if not changes:
        raise ValueError("No supported official Qwen token prices were found")
    historical = sorted(name for name in catalog if name.startswith(("qwen", "qwq")) and name not in seen)
    # Validate the full response before changing any existing catalog entry.
    catalog.update(changes)
    for name in historical:
        catalog[name] = dict(catalog[name], official_api_verification_status="not_listed",
                             official_api_checked_at=collected_at,
                             official_api_verification_note="This exact model ID is absent from the current official API pricing response. Historical prices and their original attribution are retained; they are not verified official list prices from this update.")
    return {"imported": sorted(changes), "unsupported": unsupported, "historical_unlisted": historical,
            "reference": REFERENCE, "pricing_basis": "list_before_discount", "collected_at":collected_at}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dataset", type=Path, help="Saved official ListModelSeries response; otherwise fetch it")
    parser.add_argument("--catalog", type=Path, default=CATALOG)
    parser.add_argument("--collected-at", default=date.today().isoformat())
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    if args.dataset:
        source = args.dataset.read_text(encoding="utf-8-sig")
    else:
        params = {"product":"AliyunDeliveryService", "action":"ListModelSeries", "sec_token":"",
                  "params":json.dumps({"PageNo":1,"PageSize":200,"OrderType":"Featured","Language":"zh-CN","ModelRegion":"cn-beijing"})}
        request = Request(ENDPOINT, data=urlencode(params).encode(), headers={"User-Agent":"Sub2API-Qwen-Pricing-Import/1.0"})
        with urlopen(request,timeout=45) as response:
            source = response.read().decode("utf-8")
    catalog = json.loads(args.catalog.read_text(encoding="utf-8-sig"))
    result = import_models(catalog, json.loads(source), args.collected_at)
    args.catalog.write_text(json.dumps(catalog,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
    if args.report:
        args.report.write_text(json.dumps(result,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
    print(json.dumps(result,ensure_ascii=False,indent=2))


if __name__ == "__main__":
    main()
