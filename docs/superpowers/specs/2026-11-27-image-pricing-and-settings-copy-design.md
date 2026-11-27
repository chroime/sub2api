# GPT image unit pricing and UI refinements

The GPT Image group on server 15 has image-count billing enabled, image rate independence at 1x, and 0.03 prices for 1K/2K/4K. Its five native GPT image models currently inherit token catalog or channel prices in the model plaza, so the UI shows per-million-token input/cache figures instead of the actual per-image charge.

The plaza must resolve native GPT image models through the existing billed image-unit resolver when their group enables count billing and the otherwise selected price is token based or missing. Explicit group model cards with image or per-request pricing retain priority. Uniform image-size prices collapse to one per-image price; distinct prices retain their size labels. Official token quotes retain their units and do not become per-image comparisons. Image-independent rates apply once, and media-only tables omit token-unit notes.

The channel status page must show its three metric summaries without native hover hints. All routes to its monitor detail dialog are removed, including the dialog mount and card event forwarding. The cards become static articles.

Payment settings replace the misleading disabled-draft wording with New receiving account and Save without enabling, including the associated Chinese and English guidance.

Validation includes the production-like five-model plaza case for both catalog and channel token pricing, zero and distinct size prices, switch and text-model boundaries, explicit model card precedence, frontend image-rate/unit comparisons, channel click behavior, and existing payment component coverage. The application pricing calculation and saved production price configuration are unchanged by these display changes.
