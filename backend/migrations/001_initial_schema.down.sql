-- 001_initial_schema (down) — drop in reverse dependency order so FK constraints
-- never block a drop. Reverts the up migration cleanly.
DROP TABLE IF EXISTS compliance_record_warnings;
DROP TABLE IF EXISTS compliance_record;
DROP TABLE IF EXISTS rule_warning_templates;
DROP TABLE IF EXISTS classification_rule;
DROP TABLE IF EXISTS product;
DROP TABLE IF EXISTS warning_template;
DROP TABLE IF EXISTS entity;
