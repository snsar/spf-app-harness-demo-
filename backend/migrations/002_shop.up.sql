-- 002_shop (up) — per-shop OAuth install state for the multi-tenant app (F3).
-- Each installed Shopify store gets exactly one row keyed by its myshopify domain.
-- access_token is the offline Admin API token: SECRET — never logged, never
-- returned over the API. CREATE TABLE IF NOT EXISTS keeps the up leg idempotent
-- (MySQL DDL is non-transactional, so re-runs must not error).
CREATE TABLE IF NOT EXISTS shop (
  id           BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  shop_domain  VARCHAR(255) NOT NULL,            -- <shop>.myshopify.com (tenant key)
  access_token VARCHAR(512) NULL,                -- Shopify offline token (SECRET)
  scope        VARCHAR(512) NULL,                -- granted scope string
  installed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_shop_domain (shop_domain)        -- one row per shop; upsert target
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
