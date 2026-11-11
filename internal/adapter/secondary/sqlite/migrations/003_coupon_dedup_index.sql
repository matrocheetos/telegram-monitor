CREATE INDEX IF NOT EXISTS idx_coupons_store_code_created ON coupons(store_name, code, created_at DESC);
