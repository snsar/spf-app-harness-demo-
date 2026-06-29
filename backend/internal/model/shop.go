package model

// Shop is one installed Shopify store (a tenant). It backs the `shop` table
// (migration 002). All product/rules/compliance data is scoped to a Shop.
//
// AccessToken is the Shopify offline Admin API token and is SECRET: it must
// never be logged and never serialized to an API response. There is therefore
// deliberately no `json` tag that would expose it — the only field published
// over the API is ShopDomain (see handler /api/me).
type Shop struct {
	ID          int64  `json:"id"`
	ShopDomain  string `json:"shop_domain"`
	AccessToken string `json:"-"` // SECRET — never serialized
	Scope       string `json:"-"`
	InstalledAt string `json:"installed_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}
