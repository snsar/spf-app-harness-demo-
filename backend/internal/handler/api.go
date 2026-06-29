// Package handler — api.go wires the shop-scoped REST API consumed by the React
// admin. Every route resolves the shop via ShopFromContext (set by
// RequireSessionToken) and passes shop.ID to the service/repository so no query
// can read another tenant's rows. Handlers stay thin: validate, call, map to HTTP.
//
// Status mapping: validation -> 400; not-found / cross-shop -> 404 (never reveal
// another tenant's row exists); referenced-delete -> 409; auth -> 401 (middleware).
package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gpsr/backend/internal/model"
	"github.com/gpsr/backend/internal/repository"
)

// --- persistence/service ports (satisfied by the concrete repos/services) -----

type productLister interface {
	ListWithCompliance(ctx context.Context, shopID int64, page, limit int) ([]model.ProductWithCompliance, bool, error)
	// GetWithCompliance returns a single product within the shop, or nil for an
	// unknown / cross-shop id. Used to enforce product-ownership on writes.
	GetWithCompliance(ctx context.Context, shopID, productID int64) (*model.ProductWithCompliance, error)
}

type entityStore interface {
	Create(ctx context.Context, shopID int64, e model.Entity) (*model.Entity, error)
	Get(ctx context.Context, shopID, id int64) (*model.Entity, error)
	List(ctx context.Context, shopID int64) ([]model.Entity, error)
	Update(ctx context.Context, shopID, id int64, e model.Entity) (*model.Entity, error)
	Delete(ctx context.Context, shopID, id int64) error
}

type warningStore interface {
	Create(ctx context.Context, shopID int64, w model.WarningTemplate) (*model.WarningTemplate, error)
	Get(ctx context.Context, shopID, id int64) (*model.WarningTemplate, error)
	List(ctx context.Context, shopID int64) ([]model.WarningTemplate, error)
	Update(ctx context.Context, shopID, id int64, w model.WarningTemplate) (*model.WarningTemplate, error)
	Delete(ctx context.Context, shopID, id int64) error
}

type ruleStore interface {
	Create(ctx context.Context, shopID int64, r model.Rule) (*model.Rule, error)
	Get(ctx context.Context, shopID, id int64) (*model.Rule, error)
	List(ctx context.Context, shopID int64) ([]model.Rule, error)
	Update(ctx context.Context, shopID, id int64, r model.Rule) (*model.Rule, error)
	Delete(ctx context.Context, shopID, id int64) error
	EntityBelongsToShop(ctx context.Context, shopID, entityID int64) (bool, error)
	WarningTemplatesBelongToShop(ctx context.Context, shopID int64, ids []int64) (bool, error)
}

type classifier interface {
	ApplyRuleset(ctx context.Context, shopID int64, productIDs []int64, rules []model.Rule) error
	SetOverride(ctx context.Context, shopID, productID, entityID int64, warningTemplateIDs []int64) error
	ClearOverride(ctx context.Context, shopID, productID int64) error
}

// complianceReader loads a single compliance record for a product within a shop.
// Used by the metafield-sync path after ApplyRuleset commits.
type complianceReader interface {
	GetRecord(ctx context.Context, shopID, productID int64) (*model.ComplianceRecord, error)
}

// shopifyProductIDGetter looks up the Shopify product id (the remote ID stored
// in the product table) for a local surrogate product id. Used by the
// metafield-sync path to form the Shopify product GID.
type shopifyProductIDGetter interface {
	GetShopifyProductID(ctx context.Context, shopID, productID int64) (int64, error)
}

// productSyncer pulls products from Shopify for a shop (POST /api/sync).
type productSyncer interface {
	SyncProducts(ctx context.Context, shop *model.Shop) (int, error)
}

// MetafieldWriter is the port for writing GPSR compliance outcomes to Shopify
// product metafields (app namespace). It is called after every DB commit as a
// best-effort side-effect: failures are non-fatal (never roll back the DB write).
// Satisfied by *service.ShopifyMetafieldService (live HTTP) or a test fake.
//
// The handler calls this with the resolved entity and warnings it already holds
// from the classify/override operation. For apply (bulk), the handler reads back
// the compliance records after the DB commit to resolve entity + warnings.
type MetafieldWriter interface {
	WriteComplianceMetafields(
		ctx context.Context,
		shopID int64,
		shopifyProductID int64,
		status model.Status,
		entity *model.Entity, // nil for needs_review
		warnings []string,    // nil/empty for needs_review
	) error
}

// APIDeps carries the API handlers' dependencies.
type APIDeps struct {
	Products        productLister
	Entities        entityStore
	Warnings        warningStore
	Rules           ruleStore
	Classifier      classifier
	Sync            productSyncer          // optional; /api/sync returns 503 if nil
	MetafieldWriter MetafieldWriter        // optional; nil disables metafield sync
	ComplianceRecs  complianceReader       // optional; needed for apply metafield sync
	ShopifyProdIDs  shopifyProductIDGetter // optional; needed for apply metafield sync
}

// RegisterAPIRoutes mounts all shop-scoped REST routes on the (protected) group.
func RegisterAPIRoutes(r gin.IRouter, d APIDeps) {
	r.GET("/products", d.listProducts)

	r.GET("/entities", d.listEntities)
	r.POST("/entities", d.createEntity)
	r.GET("/entities/:id", d.getEntity)
	r.PUT("/entities/:id", d.updateEntity)
	r.DELETE("/entities/:id", d.deleteEntity)

	r.GET("/warning-templates", d.listWarnings)
	r.POST("/warning-templates", d.createWarning)
	r.GET("/warning-templates/:id", d.getWarning)
	r.PUT("/warning-templates/:id", d.updateWarning)
	r.DELETE("/warning-templates/:id", d.deleteWarning)

	r.GET("/rules", d.listRules)
	r.POST("/rules", d.createRule)
	r.GET("/rules/:id", d.getRule)
	r.PUT("/rules/:id", d.updateRule)
	r.DELETE("/rules/:id", d.deleteRule)

	r.POST("/compliance/apply", d.applyRuleset)
	r.POST("/compliance/override", d.setOverride)
	r.DELETE("/compliance/override/:product_id", d.clearOverride)

	r.POST("/sync", d.syncProducts)
}

// shopID resolves the authenticated shop; on a missing context it 401s and
// returns ok=false so the handler aborts.
func shopID(c *gin.Context) (int64, bool) {
	shop, ok := ShopFromContext(c)
	if !ok || shop == nil {
		unauthorized(c)
		return 0, false
	}
	return shop.ID, true
}

func badRequest(c *gin.Context, msg string) { c.JSON(http.StatusBadRequest, gin.H{"error": msg}) }
func notFound(c *gin.Context)               { c.JSON(http.StatusNotFound, gin.H{"error": "not found"}) }
func serverError(c *gin.Context)            { c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"}) }

// pathID parses a positive int64 path param, or returns ok=false (treated as 404).
func pathID(c *gin.Context, name string) (int64, bool) {
	v, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// --- products -----------------------------------------------------------------

func (d APIDeps) listProducts(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	items, hasNext, err := d.Products.ListWithCompliance(c.Request.Context(), sid, page, limit)
	if err != nil {
		serverError(c)
		return
	}
	// Serialize each product with its compliance (never expose shop_id).
	products := make([]gin.H, 0, len(items))
	for _, it := range items {
		products = append(products, productJSON(it))
	}
	c.JSON(http.StatusOK, gin.H{
		"products": products,
		"page":     page,
		"has_next": hasNext,
	})
}

// productJSON renders a ProductWithCompliance to the published shape (snake_case,
// surrogate id, compliance null when absent — C2).
func productJSON(it model.ProductWithCompliance) gin.H {
	p := it.Product
	return gin.H{
		"id":         p.ID,
		"title":      p.Title,
		"tags":       emptySlice(p.Tags),
		"category":   p.Category,
		"material":   p.Material,
		"origin":     p.Origin,
		"compliance": it.Compliance,
	}
}

func emptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// --- entities -----------------------------------------------------------------

func (d APIDeps) listEntities(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	list, err := d.Entities.List(c.Request.Context(), sid)
	if err != nil {
		serverError(c)
		return
	}
	if list == nil {
		list = []model.Entity{}
	}
	c.JSON(http.StatusOK, gin.H{"entities": list})
}

func (d APIDeps) createEntity(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	var in model.Entity
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "invalid entity body")
		return
	}
	created, err := d.Entities.Create(c.Request.Context(), sid, in)
	if err != nil {
		serverError(c)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (d APIDeps) getEntity(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	e, err := d.Entities.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if e == nil {
		notFound(c)
		return
	}
	c.JSON(http.StatusOK, e)
}

func (d APIDeps) updateEntity(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	// Verify existence within the shop first (cross-shop -> 404).
	existing, err := d.Entities.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if existing == nil {
		notFound(c)
		return
	}
	var in model.Entity
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "invalid entity body")
		return
	}
	updated, err := d.Entities.Update(c.Request.Context(), sid, id, in)
	if err != nil {
		serverError(c)
		return
	}
	if updated == nil {
		notFound(c)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (d APIDeps) deleteEntity(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	existing, err := d.Entities.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if existing == nil {
		notFound(c)
		return
	}
	if err := d.Entities.Delete(c.Request.Context(), sid, id); err != nil {
		if errors.Is(err, repository.ErrReferenced) {
			c.JSON(http.StatusConflict, gin.H{"error": "entity is referenced by a classification rule"})
			return
		}
		serverError(c)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- warning templates --------------------------------------------------------

func (d APIDeps) listWarnings(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	list, err := d.Warnings.List(c.Request.Context(), sid)
	if err != nil {
		serverError(c)
		return
	}
	if list == nil {
		list = []model.WarningTemplate{}
	}
	c.JSON(http.StatusOK, gin.H{"warning_templates": list})
}

func (d APIDeps) createWarning(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	var in model.WarningTemplate
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "invalid warning template body")
		return
	}
	created, err := d.Warnings.Create(c.Request.Context(), sid, in)
	if err != nil {
		serverError(c)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (d APIDeps) getWarning(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	w, err := d.Warnings.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if w == nil {
		notFound(c)
		return
	}
	c.JSON(http.StatusOK, w)
}

func (d APIDeps) updateWarning(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	existing, err := d.Warnings.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if existing == nil {
		notFound(c)
		return
	}
	var in model.WarningTemplate
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "invalid warning template body")
		return
	}
	updated, err := d.Warnings.Update(c.Request.Context(), sid, id, in)
	if err != nil {
		serverError(c)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (d APIDeps) deleteWarning(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	existing, err := d.Warnings.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if existing == nil {
		notFound(c)
		return
	}
	if err := d.Warnings.Delete(c.Request.Context(), sid, id); err != nil {
		if errors.Is(err, repository.ErrReferenced) {
			c.JSON(http.StatusConflict, gin.H{"error": "warning template is referenced by a classification rule"})
			return
		}
		serverError(c)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- rules --------------------------------------------------------------------

func (d APIDeps) listRules(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	list, err := d.Rules.List(c.Request.Context(), sid)
	if err != nil {
		serverError(c)
		return
	}
	if list == nil {
		list = []model.Rule{}
	}
	c.JSON(http.StatusOK, gin.H{"rules": list})
}

// validateRuleRefs checks the rule's entity_id and warning ids all belong to the
// shop (multi-tenant ref integrity). Returns a non-empty message on failure.
func (d APIDeps) validateRuleRefs(ctx context.Context, sid int64, r model.Rule) string {
	if r.EntityID != 0 {
		ok, err := d.Rules.EntityBelongsToShop(ctx, sid, r.EntityID)
		if err != nil {
			return "could not validate entity"
		}
		if !ok {
			return "entity_id does not belong to this shop"
		}
	}
	if len(r.WarningTemplateIDs) > 0 {
		ok, err := d.Rules.WarningTemplatesBelongToShop(ctx, sid, r.WarningTemplateIDs)
		if err != nil {
			return "could not validate warning templates"
		}
		if !ok {
			return "a warning_template_id does not belong to this shop"
		}
	}
	return ""
}

func (d APIDeps) createRule(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	var in model.Rule
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "invalid rule body")
		return
	}
	if msg := d.validateRuleRefs(c.Request.Context(), sid, in); msg != "" {
		badRequest(c, msg)
		return
	}
	created, err := d.Rules.Create(c.Request.Context(), sid, in)
	if err != nil {
		serverError(c)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (d APIDeps) getRule(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	r, err := d.Rules.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if r == nil {
		notFound(c)
		return
	}
	c.JSON(http.StatusOK, r)
}

func (d APIDeps) updateRule(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	existing, err := d.Rules.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if existing == nil {
		notFound(c)
		return
	}
	var in model.Rule
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "invalid rule body")
		return
	}
	if msg := d.validateRuleRefs(c.Request.Context(), sid, in); msg != "" {
		badRequest(c, msg)
		return
	}
	updated, err := d.Rules.Update(c.Request.Context(), sid, id, in)
	if err != nil {
		serverError(c)
		return
	}
	if updated == nil {
		notFound(c)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (d APIDeps) deleteRule(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "id")
	if !ok {
		notFound(c)
		return
	}
	existing, err := d.Rules.Get(c.Request.Context(), sid, id)
	if err != nil {
		serverError(c)
		return
	}
	if existing == nil {
		notFound(c)
		return
	}
	if err := d.Rules.Delete(c.Request.Context(), sid, id); err != nil {
		serverError(c)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- compliance ---------------------------------------------------------------

type applyRequest struct {
	ProductIDs []int64 `json:"product_ids"`
}

func (d APIDeps) applyRuleset(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	var req applyRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			badRequest(c, "invalid apply body")
			return
		}
	}

	rules, err := d.Rules.List(c.Request.Context(), sid)
	if err != nil {
		serverError(c)
		return
	}

	ids := req.ProductIDs
	if len(ids) == 0 {
		// Apply to all the shop's products (the bulk default).
		ids, err = d.allProductIDs(c.Request.Context(), sid)
		if err != nil {
			serverError(c)
			return
		}
	}

	// DB commit: this is the primary operation. Never fail it due to metafield issues.
	if err := d.Classifier.ApplyRuleset(c.Request.Context(), sid, ids, rules); err != nil {
		serverError(c)
		return
	}

	// Metafield sync — best-effort after DB commit. Failure is non-fatal: we
	// return 200 with a warning field so the admin UI can surface it (Q2-A).
	metafieldWarn := d.syncMetafieldsForProducts(c.Request.Context(), sid, ids)

	resp := gin.H{"applied": len(ids)}
	if metafieldWarn != "" {
		resp["metafield_sync_warning"] = metafieldWarn
	}
	c.JSON(http.StatusOK, resp)
}

// syncMetafieldsForProducts writes the compliance metafields for a list of
// product surrogate IDs after ApplyRuleset commits. Returns a non-empty warning
// message if any metafield write failed. Never panics; always best-effort.
//
// The method skips silently if MetafieldWriter or ComplianceRecs or ShopifyProdIDs
// are not configured (optional deps for unit tests that don't need metafield sync).
func (d APIDeps) syncMetafieldsForProducts(ctx context.Context, shopID int64, productIDs []int64) string {
	if d.MetafieldWriter == nil || d.ComplianceRecs == nil || d.ShopifyProdIDs == nil {
		return ""
	}
	var lastErr error
	failCount := 0
	for _, pid := range productIDs {
		if err := d.syncOneProductMetafield(ctx, shopID, pid); err != nil {
			lastErr = err
			failCount++
		}
	}
	if lastErr != nil {
		return fmt.Sprintf("metafield sync failed for %d product(s): %s — re-classify to sync", failCount, lastErr.Error())
	}
	return ""
}

// syncOneProductMetafield reads the compliance record for one product and calls
// the metafield writer. It resolves the entity object and warning texts from their
// stores. For needs_review products, entity and warnings are nil/empty.
func (d APIDeps) syncOneProductMetafield(ctx context.Context, shopID, productID int64) error {
	rec, err := d.ComplianceRecs.GetRecord(ctx, shopID, productID)
	if err != nil || rec == nil {
		return nil // no record to sync (product unknown or not yet classified)
	}

	shopifyProdID, err := d.ShopifyProdIDs.GetShopifyProductID(ctx, shopID, productID)
	if err != nil {
		return fmt.Errorf("resolve shopify_product_id for product %d: %w", productID, err)
	}

	var entity *model.Entity
	var warnings []string

	if rec.Status != model.StatusNeedsReview && rec.EntityID != nil {
		entity, err = d.Entities.Get(ctx, shopID, *rec.EntityID)
		if err != nil {
			return fmt.Errorf("load entity %d: %w", *rec.EntityID, err)
		}
		// Resolve warning texts from template IDs.
		for _, wtID := range rec.WarningTemplateIDs {
			wt, wtErr := d.Warnings.Get(ctx, shopID, wtID)
			if wtErr != nil {
				return fmt.Errorf("load warning template %d: %w", wtID, wtErr)
			}
			if wt != nil {
				warnings = append(warnings, wt.Text)
			}
		}
	}

	return d.MetafieldWriter.WriteComplianceMetafields(ctx, shopID, shopifyProdID, rec.Status, entity, warnings)
}

// allProductIDs returns every surrogate product id for the shop (paged through
// the lister so the bulk-apply default covers all products).
func (d APIDeps) allProductIDs(ctx context.Context, sid int64) ([]int64, error) {
	var ids []int64
	page := 1
	for {
		items, hasNext, err := d.Products.ListWithCompliance(ctx, sid, page, 250)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			ids = append(ids, it.Product.ID)
		}
		if !hasNext {
			break
		}
		page++
	}
	return ids, nil
}

type overrideRequest struct {
	ProductID          int64   `json:"product_id"`
	EntityID           int64   `json:"entity_id"`
	WarningTemplateIDs []int64 `json:"warning_template_ids"`
}

func (d APIDeps) setOverride(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	var req overrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid override body")
		return
	}
	if req.ProductID <= 0 || req.EntityID <= 0 {
		badRequest(c, "product_id and entity_id are required")
		return
	}
	// Validate the product belongs to the shop (spec §4.6). The product FK
	// references the global surrogate product(id), so without this check shop A
	// could write a compliance_record pointing at shop B's product. Cross-shop /
	// unknown ids map to 404 (never reveal another tenant's row exists), matching
	// the rest of the API's cross-shop convention.
	if owned, err := d.Products.GetWithCompliance(c.Request.Context(), sid, req.ProductID); err != nil {
		serverError(c)
		return
	} else if owned == nil {
		notFound(c)
		return
	}
	// Validate entity + warnings belong to the shop (reuse the rule-ref checks).
	if ok, err := d.Rules.EntityBelongsToShop(c.Request.Context(), sid, req.EntityID); err != nil {
		serverError(c)
		return
	} else if !ok {
		badRequest(c, "entity_id does not belong to this shop")
		return
	}
	if ok, err := d.Rules.WarningTemplatesBelongToShop(c.Request.Context(), sid, req.WarningTemplateIDs); err != nil {
		serverError(c)
		return
	} else if !ok {
		badRequest(c, "a warning_template_id does not belong to this shop")
		return
	}

	// DB commit first (primary operation).
	if err := d.Classifier.SetOverride(c.Request.Context(), sid, req.ProductID, req.EntityID, req.WarningTemplateIDs); err != nil {
		serverError(c)
		return
	}

	// Metafield sync — best-effort after DB commit.
	metafieldWarn := d.syncMetafieldsForProducts(c.Request.Context(), sid, []int64{req.ProductID})

	resp := gin.H{
		"product_id":           req.ProductID,
		"entity_id":            req.EntityID,
		"status":               string(model.StatusOverride),
		"matched_rule_id":      nil,
		"warning_template_ids": emptyInt64(req.WarningTemplateIDs),
	}
	if metafieldWarn != "" {
		resp["metafield_sync_warning"] = metafieldWarn
	}
	c.JSON(http.StatusOK, resp)
}

func emptyInt64(s []int64) []int64 {
	if s == nil {
		return []int64{}
	}
	return s
}

func (d APIDeps) clearOverride(c *gin.Context) {
	sid, ok := shopID(c)
	if !ok {
		return
	}
	id, ok := pathID(c, "product_id")
	if !ok {
		notFound(c)
		return
	}
	if err := d.Classifier.ClearOverride(c.Request.Context(), sid, id); err != nil {
		serverError(c)
		return
	}
	c.Status(http.StatusNoContent) // idempotent clear (204 even if none existed)
}

// --- sync ---------------------------------------------------------------------

func (d APIDeps) syncProducts(c *gin.Context) {
	shop, ok := ShopFromContext(c)
	if !ok || shop == nil {
		unauthorized(c)
		return
	}
	if d.Sync == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sync not configured"})
		return
	}
	n, err := d.Sync.SyncProducts(c.Request.Context(), shop)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "sync failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"synced": n})
}
