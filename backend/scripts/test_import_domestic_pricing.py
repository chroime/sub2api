import unittest

from import_domestic_pricing import import_models, per_token, price_fields


class ImportDomesticPricingTests(unittest.TestCase):
    def model(self, **changes):
        model = {
            "id": "mimo-v2-6-pro", "country": "CN", "billing_currency": "CNY",
            "vendor_id": "xiaomi", "source": "https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go",
            "input_price_orig_per_m": 3, "output_price_orig_per_m": 6,
            "cached_input_price_orig_per_m": 0.025,
            "input_price_cny_per_m": 999, "output_price_cny_per_m": 999,
        }
        return model | changes

    def index(self, currency="CNY"):
        return f'<tr data-origin="cn" data-currency="{currency}"><td><a href="/tools/llm-explorer/mimo-v2-6-pro">MiMo</a></td></tr>'

    def test_native_values_win_display_conversion_and_keep_cache_precision(self):
        catalog = {}
        import_models(catalog, self.index(), {"models": [self.model()]})
        self.assertEqual(3e-6, catalog["mimo-v2.6-pro"]["input_cost_per_token"])
        self.assertEqual(0.025e-6, catalog["mimo-v2.6-pro"]["cache_read_input_token_cost"])

    def test_usd_only_reference_is_not_an_official_cny_quote(self):
        with self.assertRaisesRegex(ValueError, "no native CNY"):
            import_models({}, self.index("USD"), {"models": [self.model()]})
        with self.assertRaisesRegex(ValueError, "not an active native CNY"):
            import_models({}, self.index(), {"models": [self.model(billing_currency="USD")]})

    def test_unknown_cache_is_omitted_and_explicit_free_is_preserved(self):
        self.assertNotIn("cache_read_input_token_cost", price_fields(self.model(cached_input_price_orig_per_m=None)))
        self.assertEqual(0, price_fields(self.model(cached_input_price_orig_per_m=0))["cache_read_input_token_cost"])

    def test_direct_vendor_quote_is_preserved(self):
        catalog = {"mimo-v2.6-pro": {"currency": "CNY", "input_cost_per_token": 4e-6}}
        import_models(catalog, self.index(), {"models": [self.model()]})
        self.assertEqual(4e-6, catalog["mimo-v2.6-pro"]["input_cost_per_token"])

    def test_deepseek_catalog_keeps_reference_quote_instead_of_off_peak_half(self):
        catalog = {"deepseek-v4-pro": {"currency": "CNY", "input_cost_per_token": 4.5e-6}}
        index = self.index().replace("mimo-v2-6-pro", "deepseek-v4-pro")
        model = self.model(id="deepseek-v4-pro", vendor_id="deepseek",
                           input_price_orig_per_m=9, output_price_orig_per_m=27,
                           cached_input_price_orig_per_m=0.3)
        import_models(catalog, index, {"models": [model]})
        self.assertEqual(9e-6, catalog["deepseek-v4-pro"]["input_cost_per_token"])
        self.assertEqual(27e-6, catalog["deepseek-v4-pro"]["output_cost_per_token"])
        self.assertEqual(0.3e-6, catalog["deepseek-v4-pro"]["cache_read_input_token_cost"])
        self.assertTrue(catalog["deepseek-v4-pro"]["deepseek_peak_price"])

    def test_primary_verified_deepseek_quote_survives_secondary_import(self):
        import copy
        official = {
            "currency": "CNY", "input_cost_per_token": 9e-6,
            "output_cost_per_token": 27e-6, "cache_creation_input_token_cost": 0,
            "official_api_verification_status": "verified",
            "pricing_reference": "https://api-docs.deepseek.com/zh-cn/quick_start/pricing/",
        }
        catalog = {"deepseek-v4-pro": copy.deepcopy(official)}
        index = self.index().replace("mimo-v2-6-pro", "deepseek-v4-pro")
        secondary = self.model(id="deepseek-v4-pro", vendor_id="deepseek", input_price_orig_per_m=999)
        report = import_models(catalog, index, {"models": [secondary]})
        self.assertEqual(official, catalog["deepseek-v4-pro"])
        self.assertEqual(["deepseek-v4-pro"], report["preserved_direct_vendor_prices"])

    def test_bad_prices_are_rejected(self):
        for value in [-1, "NaN", "Infinity"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                per_token(value)

    def test_all_context_tiers_are_retained(self):
        tiers = []
        for limit, price in [(128000, 0.2), (256000, 0.8), (None, 1.2)]:
            tiers.append({"up_to_input_tokens": limit, "input_price_orig_per_m": price, "output_price_orig_per_m": price * 10})
        result = price_fields(self.model(price_tiers=tiers))["context_pricing_tiers"]
        self.assertEqual(3, len(result))
        self.assertEqual(256000, result[-1]["min_tokens"])
        self.assertEqual(1.2e-6, result[-1]["input_price"])
        self.assertNotIn("max_tokens", result[-1])


if __name__ == "__main__":
    unittest.main()
