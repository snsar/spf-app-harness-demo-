-- 003_shop_scoping (up) — multi-tenant scoping (F3b).
-- Adds shop_id BIGINT NOT NULL + FK -> shop(id) ON DELETE CASCADE to the five
-- domain tables and reworks uniqueness/precedence keys to be per-shop.
--
-- Q1 = Option A: product gets an auto-increment surrogate `id` PK and a separate
-- `shopify_product_id BIGINT NOT NULL`, with UNIQUE(shop_id, shopify_product_id).
-- compliance_record.product_id keeps FKing product(id) — now the surrogate.
--
-- MySQL DDL is NON-TRANSACTIONAL: each statement commits independently, so this
-- file is written to run once on a clean (CI) schema. There is no production
-- data (pre-launch), so shop_id is added NOT NULL directly with no backfill.
-- The down leg restores the original single-tenant shape and is IF-safe.

-- 1. entity: add shop_id + FK + index.
ALTER TABLE entity
  ADD COLUMN shop_id BIGINT NOT NULL,
  ADD KEY idx_entity_shop (shop_id),
  ADD CONSTRAINT fk_entity_shop FOREIGN KEY (shop_id)
    REFERENCES shop (id) ON DELETE CASCADE ON UPDATE CASCADE;

-- 2. warning_template: add shop_id + FK + index.
ALTER TABLE warning_template
  ADD COLUMN shop_id BIGINT NOT NULL,
  ADD KEY idx_warning_template_shop (shop_id),
  ADD CONSTRAINT fk_warning_template_shop FOREIGN KEY (shop_id)
    REFERENCES shop (id) ON DELETE CASCADE ON UPDATE CASCADE;

-- 3. classification_rule: add shop_id + FK; precedence is per-shop, so the
-- priority index becomes (shop_id, priority). The entity FK is unchanged (RESTRICT, C5).
ALTER TABLE classification_rule
  ADD COLUMN shop_id BIGINT NOT NULL,
  DROP KEY idx_rule_priority,
  ADD KEY idx_rule_shop_priority (shop_id, priority),
  ADD CONSTRAINT fk_rule_shop FOREIGN KEY (shop_id)
    REFERENCES shop (id) ON DELETE CASCADE ON UPDATE CASCADE;

-- 4. product PK rework (Q1 Option A). compliance_record.product_id FKs product(id),
-- so that FK must be dropped before product's PK is altered, then re-added.
ALTER TABLE compliance_record
  DROP FOREIGN KEY fk_record_product;

-- Rename the existing Shopify-id PK column to shopify_product_id, drop its PK
-- role, and introduce a fresh auto-increment surrogate id PK.
ALTER TABLE product
  DROP PRIMARY KEY,
  CHANGE COLUMN id shopify_product_id BIGINT NOT NULL,
  ADD COLUMN id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY FIRST,
  ADD COLUMN shop_id BIGINT NOT NULL,
  ADD UNIQUE KEY uq_product_shop_shopify (shop_id, shopify_product_id),
  ADD KEY idx_product_shop (shop_id),
  ADD CONSTRAINT fk_product_shop FOREIGN KEY (shop_id)
    REFERENCES shop (id) ON DELETE CASCADE ON UPDATE CASCADE;

-- 5. compliance_record: add shop_id + FK; one record per (shop, product); index
-- (shop_id, status). Re-add the product FK against the new surrogate product.id.
--
-- entity_id rework: in F1 it was NOT NULL + RESTRICT, but a needs_review record
-- has NO responsible entity (C2 — invent nothing), so it must be NULLable. The
-- entity FK becomes ON DELETE SET NULL so (a) needs_review records persist with a
-- NULL entity and (b) a full-shop teardown (DELETE shop -> cascade entity) does
-- not deadlock against this record's reference. The C5 guarantee (a referenced
-- entity cannot be deleted via the API) is enforced by the rule -> entity RESTRICT
-- FK, which the 409 path exercises — not by this record-level FK.
ALTER TABLE compliance_record
  DROP FOREIGN KEY fk_record_entity,
  MODIFY COLUMN entity_id BIGINT NULL;
ALTER TABLE compliance_record
  ADD CONSTRAINT fk_record_entity FOREIGN KEY (entity_id)
    REFERENCES entity (id) ON DELETE SET NULL ON UPDATE CASCADE,
  ADD COLUMN shop_id BIGINT NOT NULL,
  DROP KEY uq_record_product,
  DROP KEY idx_record_status,
  ADD UNIQUE KEY uq_record_shop_product (shop_id, product_id),
  ADD KEY idx_record_shop_status (shop_id, status),
  ADD CONSTRAINT fk_record_shop FOREIGN KEY (shop_id)
    REFERENCES shop (id) ON DELETE CASCADE ON UPDATE CASCADE,
  ADD CONSTRAINT fk_record_product FOREIGN KEY (product_id)
    REFERENCES product (id) ON DELETE CASCADE ON UPDATE CASCADE;
