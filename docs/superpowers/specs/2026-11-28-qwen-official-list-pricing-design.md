# Qwen official API list prices

The user requests updating the built-in Qwen price table from https://platform.qianwenai.com/pricing/api, explicitly using prices before discounts. The page's own `AliyunDeliveryService/ListModelSeries` response is the source. Every imported amount uses `Price`, without multiplying by `Discount` or selecting Batch prices.

Update the existing shared catalog with the native CNY prices for Qwen models whose regular token prices fit its input/output/cache and context-tier fields. Use API model IDs, not display names. Preserve complete input-length ladders, original source amounts and units, collection date, and the official reference. Prefer ordinary cache-hit prices where quoted; keep explicit-cache prices distinguishable in source metadata because the runtime has a single cache-read field. When only explicit cache prices are quoted, use those verified values.

Do not collapse thinking/nonthinking price differences or turn image, audio-duration, character, or request charges into token prices. Record the affected model IDs and reasons. Existing legacy Qwen IDs absent from the official response retain their historical attributed records, with an explicit note that this update did not verify them.

Use the existing domestic importer preservation rule to protect direct official CNY entries from a later third-party import. Verify official-list values and discounted fixtures, context boundaries, cache kind, catalog runtime parsing and billing, and unchanged non-Qwen entries. Production deployment is a subsequent action.
