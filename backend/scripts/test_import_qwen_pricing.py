import copy
import unittest

from import_qwen_pricing import ENDPOINT, REFERENCE, import_models


def quote(kind, price, **values):
    return {"Type": kind, "Price": str(price), "PriceUnit": "每百万tokens", "TimeBand": "standard"} | values


def model(model_id="qwen3.7-plus", **values):
    return {"Model": model_id, "Prices": [quote("input_token", 2, Discount="0.8"), quote("output_token", 8, Discount="0.8")]} | values


def response(*models):
    return {"code": "200", "data": {"Data": [{"Name": "Qwen", "Items": list(models)}]}}


class ImportQwenPricingTests(unittest.TestCase):
    def test_no_separate_official_cache_write_charge_is_explicit_zero(self):
        catalog = {}
        import_models(catalog, response(model()), "2026-11-28")
        self.assertIn("cache_creation_input_token_cost", catalog["qwen3.7-plus"])
        self.assertEqual(0, catalog["qwen3.7-plus"]["cache_creation_input_token_cost"])

    def test_each_context_tier_preserves_free_and_paid_cache_writes(self):
        tiers = [
            {"RangeName": "输入<=128k", "Prices": [quote("input_token", 1), quote("output_token", 4)]},
            {"RangeName": "128k<输入<=256k", "Prices": [quote("input_token", 2), quote("output_token", 8), quote("input_token_cache_creation_5m", 2.5)]},
        ]
        catalog = {}
        import_models(catalog, response(model(Prices=[], MultiPrices=tiers)), "2026-11-28")
        entry = catalog["qwen3.7-plus"]
        self.assertIn("cache_creation_input_token_cost", entry)
        self.assertEqual(0, entry["cache_creation_input_token_cost"])
        self.assertEqual(0, entry["context_pricing_tiers"][0]["cache_write_price"])
        self.assertEqual(2.5e-6, entry["context_pricing_tiers"][1]["cache_write_price"])

    def test_original_prices_ignore_discounts_and_batch_quotes(self):
        entry = model(Prices=[quote("input_token", 2, Discount="0.8"), quote("output_token", 8, Discount="0.8"), quote("input_token_batch", 1), quote("output_token_batch", 4), quote("input_token", 0.01, TimeBand="off_peak")])
        catalog = {"qwen3.7-plus": {"input_cost_per_token_priority": 9, "context_pricing_tiers": [{"input_price": 99}], "pricing_data_source": "third-party"}, "gpt-existing": {"input_cost_per_token": 123}}
        report = import_models(catalog, response(entry), "2026-11-28")
        actual = catalog["qwen3.7-plus"]
        self.assertEqual(2e-6, actual["input_cost_per_token"])
        self.assertEqual(8e-6, actual["output_cost_per_token"])
        self.assertEqual("CNY", actual["currency"])
        self.assertEqual(REFERENCE, actual["pricing_reference"])
        self.assertEqual("list_before_discount", actual["pricing_basis"])
        self.assertNotIn("input_cost_per_token_priority", actual)
        self.assertNotIn("context_pricing_tiers", actual)
        self.assertEqual(ENDPOINT, actual["pricing_data_source"])
        self.assertEqual({"input_cost_per_token": 123}, catalog["gpt-existing"])
        self.assertEqual(["qwen3.7-plus"], report["imported"])

    def test_ordinary_cache_hit_wins_and_explicit_quote_remains_distinct(self):
        prices = [quote("input_token", 12), quote("output_token", 36), quote("input_token_cache", 1.5), quote("input_token_cache_read", 1), quote("input_token_cache_creation_5m", 15), quote("input_token_cache_creation_1h", 24)]
        catalog = {}
        import_models(catalog, response(model("qwen3.8-max", Prices=prices)), "2026-11-28")
        entry = catalog["qwen3.8-max"]
        self.assertEqual(1.5e-6, entry["cache_read_input_token_cost"])
        self.assertEqual(15e-6, entry["cache_creation_input_token_cost"])
        self.assertEqual(24e-6, entry["cache_creation_input_token_cost_above_1hr"])
        self.assertEqual("ordinary", entry["pricing_cache_read_kind"])
        self.assertEqual("1", entry["official_api_price_rows"][0]["prices"][3]["Price"])

    def test_all_context_tiers_keep_cache_prices_and_exact_boundaries(self):
        tiers = []
        for label, price in [("输入<=128k", 0.2), ("128k<输入<=256k", 0.8), ("256k<输入<=1m", 1.2)]:
            tiers.append({"RangeName": label, "Prices": [quote("input_token", price), quote("output_token", price * 10), quote("input_token_cache_read", price / 10), quote("input_token_cache_creation_5m", price * 1.25)]})
        catalog = {}
        import_models(catalog, response(model("qwen3.5-flash", Prices=[], MultiPrices=tiers)), "2026-11-28")
        entry = catalog["qwen3.5-flash"]
        self.assertEqual(2e-7, entry["input_cost_per_token"])
        self.assertEqual(0.02e-6, entry["cache_read_input_token_cost"])
        self.assertEqual("explicit", entry["pricing_cache_read_kind"])
        actual = entry["context_pricing_tiers"]
        self.assertEqual([0, 128000, 256000], [t["min_tokens"] for t in actual])
        self.assertEqual([128000, 256000, None], [t.get("max_tokens") for t in actual])
        self.assertEqual(0.12e-6, actual[2]["cache_read_price"])
        self.assertEqual(1.5e-6, actual[2]["cache_write_price"])

    def test_unsupported_conditions_are_reported_without_repricing(self):
        conditional = model("qwen-plus", Prices=[quote("input_token", 0.8), quote("output_token", 2), quote("thinking_input_token", 0.8), quote("thinking_output_token", 8)])
        image = model("qwen-image-3.0", Prices=[{"Type": "qima_output_1k", "Price": "0.18", "PriceUnit": "每张"}])
        historical = {"input_cost_per_token": 9e-6, "source": "historical"}
        catalog = {"qwen-plus": dict(historical), "qwen3-max": dict(historical)}
        report = import_models(catalog, response(conditional, image, model("qwen4-max"), model("other-vendor")), "2026-11-28")
        self.assertEqual(historical, catalog["qwen-plus"])
        self.assertNotIn("qwen-image-3.0", catalog)
        self.assertNotIn("other-vendor", catalog)
        self.assertEqual({"qwen-plus", "qwen-image-3.0"}, {r["model"] for r in report["unsupported"]})
        self.assertEqual(["qwen3-max"], report["historical_unlisted"])
        self.assertEqual(9e-6, catalog["qwen3-max"]["input_cost_per_token"])
        self.assertEqual("not_listed", catalog["qwen3-max"]["official_api_verification_status"])
        self.assertNotIn("pricing_reference", catalog["qwen3-max"])

    def test_invalid_units_numbers_tiers_and_api_errors_leave_catalog_intact(self):
        invalid = [
            response(model(Prices=[quote("input_token", -1), quote("output_token", 8)])),
            response(model(Prices=[quote("input_token", "NaN"), quote("output_token", 8)])),
            response(model(Prices=[quote("input_token", 2, PriceUnit="每千tokens"), quote("output_token", 8)])),
            response(model(Prices=[], MultiPrices=[{"RangeName": "128k<输入<=256k", "Prices": [quote("input_token", 2), quote("output_token", 8)]}])),
            {"code": "403", "data": {"Data": []}},
            {"code": "200", "data": {"Ext": {"totalCount": 2}, "Data": [{"Items": [model()]}]}},
        ]
        for data in invalid:
            with self.subTest(data=data):
                catalog = {"existing": {"input_cost_per_token": 1}}
                before = copy.deepcopy(catalog)
                with self.assertRaises(ValueError): import_models(catalog, data, "2026-11-28")
                self.assertEqual(before, catalog)

    def test_later_domestic_import_preserves_official_qwen_price(self):
        from import_domestic_pricing import import_models as import_domestic
        catalog = {}
        import_models(catalog, response(model("qwen3.7-max")), "2026-11-28")
        official = copy.deepcopy(catalog["qwen3.7-max"])
        index = '<tr data-origin="cn" data-currency="CNY"><td><a href="/tools/llm-explorer/qwen3-7-max">Qwen</a></td></tr>'
        data = {"models": [{"id":"qwen3-7-max","country":"CN","billing_currency":"CNY","vendor_id":"alibaba","source":"third-party","input_price_orig_per_m":999,"output_price_orig_per_m":999}]}
        report = import_domestic(catalog,index,data)
        self.assertEqual(official,catalog["qwen3.7-max"])
        self.assertEqual(["qwen3.7-max"],report["preserved_direct_vendor_prices"])


if __name__ == "__main__":
    unittest.main()
