-- 001_initial_schema (up) — GPSR Compliance Engine core data model.
-- MySQL 8.4, InnoDB, utf8mb4. snake_case columns. All FKs are explicit.
-- Referential integrity (C5): a warning_template / entity referenced by a rule
-- (or a record) cannot be silently deleted — ON DELETE RESTRICT blocks it.

-- entity: responsible economic operators the rules map products to.
CREATE TABLE entity (
  id         BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(255) NOT NULL,
  address    TEXT NOT NULL,
  role       VARCHAR(64) NOT NULL,            -- e.g. manufacturer | importer | authorised_representative
  is_eu      BOOLEAN NOT NULL DEFAULT FALSE,  -- EU-based responsible person?
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- warning_template: editable safety-warning text (data, not code). Single locale
-- for MVP but the column stays for phase-2 multi-locale. applies_to holds the
-- optional match conditions (tag/category/material/origin) as JSON.
CREATE TABLE warning_template (
  id         BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  locale     VARCHAR(16) NOT NULL DEFAULT 'en',
  text       TEXT NOT NULL,
  applies_to JSON NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- product: local mirror of Shopify products. id IS the Shopify product id
-- (not auto-increment) so sync upserts are idempotent (C7). Only the fields the
-- rules match on are mirrored. updated_at reflects the last local mirror write.
CREATE TABLE product (
  id         BIGINT NOT NULL PRIMARY KEY,      -- Shopify product id
  title      VARCHAR(512) NOT NULL,
  tags       JSON NULL,                        -- array of tag strings
  category   VARCHAR(255) NULL,
  material   VARCHAR(255) NULL,
  origin     VARCHAR(255) NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- classification_rule: ordered rule. priority is the explicit precedence integer
-- (lower = higher precedence; first match wins — C1). match_conditions is JSON
-- (tag/category/material/origin predicates) — flexible, engine-evaluated, so JSON
-- fits better than relational columns. entity_id is RESTRICT so a referenced
-- entity cannot be deleted out from under a rule (C5).
CREATE TABLE classification_rule (
  id               BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  priority         INT NOT NULL,
  match_conditions JSON NOT NULL,
  entity_id        BIGINT NOT NULL,
  created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_rule_priority (priority),
  CONSTRAINT fk_rule_entity FOREIGN KEY (entity_id)
    REFERENCES entity (id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- rule_warning_templates: many-to-many rule -> warning_template. A join table
-- (not a JSON id array) is used so the DB can enforce that a template referenced
-- by a rule cannot be deleted (C5) — JSON ids carry no FK guarantee.
CREATE TABLE rule_warning_templates (
  rule_id             BIGINT NOT NULL,
  warning_template_id BIGINT NOT NULL,
  PRIMARY KEY (rule_id, warning_template_id),
  CONSTRAINT fk_rwt_rule FOREIGN KEY (rule_id)
    REFERENCES classification_rule (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_rwt_template FOREIGN KEY (warning_template_id)
    REFERENCES warning_template (id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- compliance_record: the inference output per product. matched_rule_id is NULL
-- for a manual override or an unmatched product (audit trail — C2). status is the
-- terminal-state ENUM. Deleting a product cascades its record; deleting the
-- matched rule nulls the pointer (record + audit text remain). Deleting the
-- assigned entity is RESTRICT (a live record must keep a valid responsible person).
CREATE TABLE compliance_record (
  id              BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  product_id      BIGINT NOT NULL,
  matched_rule_id BIGINT NULL,
  entity_id       BIGINT NOT NULL,
  status          ENUM('ok','needs_review','override') NOT NULL DEFAULT 'needs_review',
  generated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_record_product (product_id),
  KEY idx_record_status (status),
  CONSTRAINT fk_record_product FOREIGN KEY (product_id)
    REFERENCES product (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_record_rule FOREIGN KEY (matched_rule_id)
    REFERENCES classification_rule (id) ON DELETE SET NULL ON UPDATE CASCADE,
  CONSTRAINT fk_record_entity FOREIGN KEY (entity_id)
    REFERENCES entity (id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- compliance_record_warnings: many-to-many record -> warning_template (the
-- warnings actually emitted on this record). Join table for the same C5 reason;
-- also preserves the audit trail of which templates were rendered. Deleting a
-- record cascades; a template still referenced by any record is RESTRICT.
CREATE TABLE compliance_record_warnings (
  compliance_record_id BIGINT NOT NULL,
  warning_template_id  BIGINT NOT NULL,
  PRIMARY KEY (compliance_record_id, warning_template_id),
  CONSTRAINT fk_crw_record FOREIGN KEY (compliance_record_id)
    REFERENCES compliance_record (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_crw_template FOREIGN KEY (warning_template_id)
    REFERENCES warning_template (id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
