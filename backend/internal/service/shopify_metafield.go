// Package service — shopify_metafield.go writes GPSR compliance outcomes to
// Shopify product metafields (app namespace "$app") via the Admin GraphQL
// metafieldsSet mutation. The Liquid storefront block reads these server-side
// with no JavaScript and no public endpoint.
//
// Security posture:
//   - Shop domain is validated with ValidateShopDomain (SSRF guard — F3b-SEC-2).
//   - The access token is loaded from the DB via ShopTokenReader, never accepted
//     from the caller (prevents caller-injected credential substitution).
//   - No token or secret is ever written to an error message or log line.
//   - Failure is non-fatal to classification: callers log and surface a warning
//     in the response body; the DB record is already committed.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gpsr/backend/internal/model"
)

// ShopTokenReader fetches a shop's Admin API credentials (domain + offline token)
// from the data store, keyed by the internal shop surrogate ID. This interface
// keeps the service layer independent of the repository package (dependency
// inversion) and makes the token lookup fakeable in unit tests.
//
// Implementations MUST NOT log the returned token under any circumstances.
type ShopTokenReader interface {
	GetShopCredentials(ctx context.Context, shopID int64) (domain, token string, err error)
}

// ShopifyMetafieldWriter is the port the API handlers call after a DB commit.
// It is satisfied by *ShopifyMetafieldService (live HTTP) and by any test fake
// that implements the same signature.
type ShopifyMetafieldWriter interface {
	WriteComplianceMetafields(
		ctx context.Context,
		shopID int64,
		shopifyProductID int64,
		status model.Status,
		entity *model.Entity, // nil for needs_review
		warnings []string,    // nil/empty for needs_review
	) error
}

// Compile-time check: the concrete service satisfies the handler interface.
var _ ShopifyMetafieldWriter = (*ShopifyMetafieldService)(nil)

// ShopifyMetafieldService is the live implementation. It builds the
// metafieldsSet GraphQL mutation and posts it to the Shopify Admin API.
// Inject via the ShopifyMetafieldWriter interface; test with a fake HTTP client
// or a fake implementation (no live Shopify call required for unit tests).
type ShopifyMetafieldService struct {
	http         *http.Client
	version      string
	shopToken    ShopTokenReader
	// baseOverride is empty in production; tests set it to an httptest.Server URL
	// so the real URL-construction path is exercised without real egress.
	baseOverride string
}

// NewShopifyMetafieldService constructs a live service.
// version is the Shopify Admin API version (e.g. "2026-04").
func NewShopifyMetafieldService(c *http.Client, version string, shopToken ShopTokenReader) *ShopifyMetafieldService {
	return &ShopifyMetafieldService{http: c, version: version, shopToken: shopToken}
}

// metafieldsSetMutation is the GraphQL mutation for writing app-owned metafields.
// The $app namespace maps to "app" in Liquid (product.metafields.app.<key>).
const metafieldsSetMutation = `mutation MetafieldsSet($metafields: [MetafieldsSetInput!]!) {
  metafieldsSet(metafields: $metafields) {
    metafields {
      key
      value
    }
    userErrors {
      field
      message
    }
  }
}`

// storefrontEntityShape is the public-safe subset written to gpsr_entity_json.
// It MUST NOT include internal fields: id, shop_id, is_eu, created_at, updated_at.
// (spec §7, constraint §3)
type storefrontEntityShape struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Role    string `json:"role"`
}

// metafieldInput matches the Shopify MetafieldsSetInput GraphQL type.
type metafieldInput struct {
	OwnerId   string `json:"ownerId"`
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Type      string `json:"type"`
}

// graphqlMetafieldsSetResponse mirrors the mutation response shape.
type graphqlMetafieldsSetResponse struct {
	Data struct {
		MetafieldsSet struct {
			Metafields []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"metafields"`
			UserErrors []struct {
				Field   []string `json:"field"`
				Message string   `json:"message"`
			} `json:"userErrors"`
		} `json:"metafieldsSet"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// WriteComplianceMetafields writes the three GPSR compliance metafields to
// Shopify for a product. The spec write-trigger table:
//
//	ok/override  → gpsr_status=ok|override, gpsr_entity_json={…}, gpsr_warnings_json=[…]
//	needs_review → gpsr_status=needs_review, gpsr_entity_json=null, gpsr_warnings_json=null
//
// Entity and warnings MUST be nil/empty when status is needs_review so no
// affirmative claim lingers on the storefront after a stale "ok" write.
//
// The shop's access token is loaded from the DB (via ShopTokenReader) — it is
// NEVER accepted from the caller. The shop domain is validated before building
// the egress URL (SSRF guard, F3b-SEC-2).
func (s *ShopifyMetafieldService) WriteComplianceMetafields(
	ctx context.Context,
	shopID int64,
	shopifyProductID int64,
	status model.Status,
	entity *model.Entity,
	warnings []string,
) error {
	// 1. Load shop credentials from DB (not from caller).
	shopDomain, accessToken, err := s.shopToken.GetShopCredentials(ctx, shopID)
	if err != nil {
		return fmt.Errorf("load shop credentials for shop %d: %w", shopID, err)
	}

	// 2. SSRF guard (F3b-SEC-2): validate domain before building URL or sending token.
	url := s.baseOverride
	if url == "" {
		if err := ValidateShopDomain(shopDomain); err != nil {
			return fmt.Errorf("invalid shop domain for metafield write: %w", err)
		}
		url = fmt.Sprintf("https://%s/admin/api/%s/graphql.json", shopDomain, s.version)
	}

	// 3. Build the Shopify product GID.
	productGID := fmt.Sprintf("gid://shopify/Product/%d", shopifyProductID)

	// 4. Build the three metafield inputs per the spec write-trigger table.
	mfs, err := buildMetafieldInputs(productGID, status, entity, warnings)
	if err != nil {
		return fmt.Errorf("build metafield inputs: %w", err)
	}

	// 5. Marshal the GraphQL request.
	reqBody, err := json.Marshal(map[string]any{
		"query":     metafieldsSetMutation,
		"variables": map[string]any{"metafields": mfs},
	})
	if err != nil {
		return fmt.Errorf("encode metafieldsSet request: %w", err)
	}

	// 6. POST to Shopify Admin GraphQL.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build metafieldsSet request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", accessToken) // DB-sourced token only

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("shopify metafieldsSet request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shopify admin returned status %d for metafieldsSet", resp.StatusCode)
	}

	// 7. Decode and check for GraphQL-level errors and userErrors.
	var gr graphqlMetafieldsSetResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return fmt.Errorf("decode metafieldsSet response: %w", err)
	}
	if len(gr.Errors) > 0 {
		return fmt.Errorf("shopify graphql error: %s", gr.Errors[0].Message)
	}
	if len(gr.Data.MetafieldsSet.UserErrors) > 0 {
		return fmt.Errorf("shopify metafieldsSet userError: %s", gr.Data.MetafieldsSet.UserErrors[0].Message)
	}

	return nil
}

// buildMetafieldInputs constructs the three MetafieldsSetInput objects.
// For needs_review, entity and warnings are set to null (empty string value
// with the appropriate type) so any stale "ok" values are overwritten.
func buildMetafieldInputs(
	productGID string,
	status model.Status,
	entity *model.Entity,
	warnings []string,
) ([]metafieldInput, error) {
	mfs := make([]metafieldInput, 0, 3)

	// gpsr_status — single_line_text_field.
	mfs = append(mfs, metafieldInput{
		OwnerId:   productGID,
		Namespace: "$app",
		Key:       "gpsr_status",
		Value:     string(status),
		Type:      "single_line_text_field",
	})

	// gpsr_entity_json — json type. null for needs_review.
	entityValue := "null"
	if status != model.StatusNeedsReview && entity != nil {
		pub := storefrontEntityShape{
			Name:    entity.Name,
			Address: entity.Address,
			Role:    entity.Role,
		}
		b, err := json.Marshal(pub)
		if err != nil {
			return nil, fmt.Errorf("marshal entity: %w", err)
		}
		entityValue = string(b)
	}
	mfs = append(mfs, metafieldInput{
		OwnerId:   productGID,
		Namespace: "$app",
		Key:       "gpsr_entity_json",
		Value:     entityValue,
		Type:      "json",
	})

	// gpsr_warnings_json — json type (array). null for needs_review.
	warningsValue := "null"
	if status != model.StatusNeedsReview {
		if len(warnings) == 0 {
			warningsValue = "[]"
		} else {
			b, err := json.Marshal(warnings)
			if err != nil {
				return nil, fmt.Errorf("marshal warnings: %w", err)
			}
			warningsValue = string(b)
		}
	}
	mfs = append(mfs, metafieldInput{
		OwnerId:   productGID,
		Namespace: "$app",
		Key:       "gpsr_warnings_json",
		Value:     warningsValue,
		Type:      "json",
	})

	return mfs, nil
}
