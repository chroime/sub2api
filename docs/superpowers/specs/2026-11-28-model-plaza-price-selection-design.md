# Model plaza price selection

The user approved separating the current mixed price-scope selector into a price view, an access group, and a model-vendor filter. The existing page combines official reference prices with named site groups in one dropdown. Choosing a group also changes the model universe; an unavailable vendor can then produce the misleading message that the whole group has no models.

Use two pressed-state buttons labelled Official reference prices and Site payable prices, grouped under View prices. Default to official prices. Only the site view shows the Access group selector, with named groups in configured order and their existing badges, personal/peak rates and monitor controls. Entering site prices selects the remembered visible group, otherwise the first group in configured order. Switching back to official prices hides/closes the group picker and remembers its selection. If a selected group disappears on refresh, choose the first remaining group while keeping the chosen site view.

The official view continues to show the deduplicated union of group catalogs with official prices. Site prices continue to show only the chosen group's actual models and existing payable-price calculations. Add a concise explanation of each view; do not expand the backend pricing or catalog API.

Keep model vendor and search selections across view/group changes. Rename the vendor row's universal choice to All vendors; show counts as model counts. A selected vendor missing from the new scope remains visible with zero models. Distinguish an actually empty group, an unavailable vendor in a group, and a search with no results. A recovery button clears vendor/search filters within the current view and group. The group description and table heading remain consistent with that scope.

Verify official/site scope transitions, remembered selection, refreshed/removed/empty groups, protected monitor requests, the Qwen-to-domestic-group mismatch, search recovery, Chinese/English completeness, production build and browser behavior at desktop/mobile widths.
