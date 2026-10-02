-- Privacy zones (spec §13.2): a private place hides the owner's location inside it from
-- everyone else (family, share links). The owner still sees their own data.
ALTER TABLE places ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
