# Qwen official API prices, 2026-11-28

The built-in catalog now uses the **prices before discounts** from [Qwen API pricing](https://platform.qianwenai.com/pricing/api) for 50 supported Qwen text-model IDs: 5 existing records updated and 45 added. Other vendors' records are unchanged.

Amounts below are CNY per million tokens. The JSON catalog stores CNY per token.

| Model | Input | Output | Ordinary cache hit | Explicit cache hit |
| --- | ---: | ---: | ---: | ---: |
| qwen4-max | 10 | 30 | 0.5 | 0.5 |
| qwen3.8-max | 12 | 36 | 1.5 | 1 |
| qwen3.8-flash | 0.8 | 2.7 | 0.1 | 0.1 |
| qwen3.7-max | 12 | 36 | 2.4 | 1.2 |
| qwen3.7-plus, input <=256K | 2 | 8 | 0.4 | 0.2 |
| qwen3.7-plus, input >256K | 6 | 24 | 1.2 | 0.6 |
| qwen3.5-plus, input <=128K | 0.8 | 4.8 | Not quoted | 0.08 |
| qwen3.5-flash, input <=128K | 0.2 | 2 | Not quoted | 0.02 |

Qwen3.7-Plus currently has a separate `Discount: "0.8"` in the source. Its stored prices remain **2/8** and **6/24**, without that discount. Batch File, Batch Chat, time discounts, and built-in tool charges do not replace the regular list prices.

The existing single cache-read field uses ordinary cache-hit prices when quoted, otherwise the quoted explicit-cache price. Exact explicit and ordinary source rows remain distinguishable in `official_api_price_rows`; `pricing_cache_read_kind` records the selected kind. Five-minute and one-hour cache-creation prices are imported where quoted. Models with context bands retain every band's input/output/cache prices; the last band follows the existing runtime's unbounded-final-band convention, with the official range labels retained in source metadata.

The source is the public `AliyunDeliveryService/ListModelSeries` endpoint used by the pricing page's versioned `@ali/hmod-qwen-api-price/0.1.19` component. The captured response contains all 129 model series and 116 Qwen model IDs. Its saved file SHA-256 is `9485b4c067b18d6ada3b37c74324b2095d48123757b5d91ceaa5d417b6adbbae`.

Three historical IDs are absent from that official response: `qwen3-max`, `qwen3.6-max-preview`, and `qwen3.8-max-prime`. Their previous prices and attribution are retained with `official_api_verification_status: not_listed`; they are not certified by this update.

The importer reports 66 source model IDs that are not imported as ordinary token cards. Five have different thinking/nonthinking prices (`qwen-plus`, `qwen-plus-latest`, `qwen-plus-2025-12-01`, `qwen-plus-2025-07-14`, `qwen-plus-2025-04-28`); 61 use other modalities or price conditions without an unambiguous regular input/output token pair. Converting those charges to a single token card would produce incorrect prices. No billing schema is extended in this data update.

To refresh from the live official source:

```powershell
python backend/scripts/import_qwen_pricing.py --collected-at 2026-11-28
```

For a captured response, pass `--dataset <ListModelSeries-response.json>` and optionally `--report <report.json>`. The importer rejects incomplete response pages, malformed prices, unsupported units and discontinuous context ranges before modifying the catalog. Existing domestic imports preserve these direct official CNY records.

Verification: all 264 imported price fields and every context boundary were independently compared with the captured official response, including all undiscounted Qwen3.7-Plus bands. All 13 Python importer tests and the focused Go pricing/catalog/context suite passed. Reimport is idempotent, the catalog parses as JSON, and all non-Qwen records remain byte-equivalent as parsed data to the starting commit.
