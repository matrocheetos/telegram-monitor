CREATE INDEX IF NOT EXISTS idx_matched_products_created_id ON matched_products(created_at, id);
CREATE INDEX IF NOT EXISTS idx_matched_products_user_created ON matched_products(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_coupons_created_id ON coupons(created_at, id);
