-- 003_shop_scoping (down) — reverts 003_shop_scoping.up.sql to the single-tenant
-- F1 shape. MySQL DDL is non-transactional; write this so it restores the
-- original keys cleanly. Order matters: drop FKs that block PK changes first.

-- 5. compliance_record: drop shop FK + product FK, drop per-shop keys, drop shop_id,
-- restore the original single-tenant unique/precedence keys, and restore the F1
-- entity FK (NOT NULL + RESTRICT).
--
-- Under 003, entity_id is NULLable (a needs_review record has no entity — C2).
-- The F1 shape requires entity_id NOT NULL, so such rows cannot survive the
-- down. Pre-launch there is no production data; we delete records that have no
-- entity before restoring NOT NULL so the migration never half-applies on
-- leftover test/dev rows (idempotent-safe down — the F1/F3 DDL caveat).
DELETE FROM compliance_record WHERE entity_id IS NULL;
ALTER TABLE compliance_record
  DROP FOREIGN KEY fk_record_shop,
  DROP FOREIGN KEY fk_record_product,
  DROP FOREIGN KEY fk_record_entity,
  DROP KEY uq_record_shop_product,
  DROP KEY idx_record_shop_status,
  DROP COLUMN shop_id,
  MODIFY COLUMN entity_id BIGINT NOT NULL,
  ADD UNIQUE KEY uq_record_product (product_id),
  ADD KEY idx_record_status (status);
ALTER TABLE compliance_record
  ADD CONSTRAINT fk_record_entity FOREIGN KEY (entity_id)
    REFERENCES entity (id) ON DELETE RESTRICT ON UPDATE CASCADE;

-- 4. product: drop shop FK + per-shop keys + shop_id + the surrogate id, restore
-- shopify_product_id back to being the BIGINT PK named `id` (F1 shape).
ALTER TABLE product
  DROP FOREIGN KEY fk_product_shop,
  DROP KEY uq_product_shop_shopify,
  DROP KEY idx_product_shop,
  DROP COLUMN shop_id,
  DROP PRIMARY KEY,
  DROP COLUMN id,
  CHANGE COLUMN shopify_product_id id BIGINT NOT NULL PRIMARY KEY;

-- Re-add compliance_record's product FK against the restored product.id PK.
ALTER TABLE compliance_record
  ADD CONSTRAINT fk_record_product FOREIGN KEY (product_id)
    REFERENCES product (id) ON DELETE CASCADE ON UPDATE CASCADE;

-- 3. classification_rule: drop shop FK, restore the original priority index.
ALTER TABLE classification_rule
  DROP FOREIGN KEY fk_rule_shop,
  DROP KEY idx_rule_shop_priority,
  DROP COLUMN shop_id,
  ADD KEY idx_rule_priority (priority);

-- 2. warning_template: drop shop FK + shop_id.
ALTER TABLE warning_template
  DROP FOREIGN KEY fk_warning_template_shop,
  DROP KEY idx_warning_template_shop,
  DROP COLUMN shop_id;

-- 1. entity: drop shop FK + shop_id.
ALTER TABLE entity
  DROP FOREIGN KEY fk_entity_shop,
  DROP KEY idx_entity_shop,
  DROP COLUMN shop_id;
