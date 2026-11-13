CREATE TABLE IF NOT EXISTS debug_analyzed_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_message_id INTEGER NOT NULL DEFAULT 0,
    channel_id INTEGER NOT NULL,
    channel_title TEXT NOT NULL DEFAULT '',
    channel_username TEXT NOT NULL DEFAULT '',
    monitor_products BOOLEAN NOT NULL DEFAULT 0,
    monitor_coupons BOOLEAN NOT NULL DEFAULT 0,
    raw_text TEXT NOT NULL,
    normalized_text TEXT NOT NULL,
    tokens_json TEXT NOT NULL DEFAULT '[]',
    has_media BOOLEAN NOT NULL DEFAULT 0,
    media_type TEXT NOT NULL DEFAULT '',
    tg_entities_json TEXT NOT NULL DEFAULT '[]',
    tg_inline_buttons_json TEXT NOT NULL DEFAULT '[]',
    tg_webpage_url TEXT NOT NULL DEFAULT '',
    plain_urls_json TEXT NOT NULL DEFAULT '[]',
    first_url TEXT NOT NULL DEFAULT '',
    all_prices_json TEXT NOT NULL DEFAULT '[]',
    prices_found_count INTEGER NOT NULL DEFAULT 0,
    extracted_lowest_price REAL NULL,
    has_coupon_indicator BOOLEAN NOT NULL DEFAULT 0,
    identified_store TEXT NOT NULL DEFAULT '',
    identified_store_url TEXT NOT NULL DEFAULT '',
    extracted_coupons_json TEXT NOT NULL DEFAULT '[]',
    extracted_coupons_count INTEGER NOT NULL DEFAULT 0,
    deduplicated_coupons_count INTEGER NOT NULL DEFAULT 0,
    matched_interests_json TEXT NOT NULL DEFAULT '[]',
    matched_products_count INTEGER NOT NULL DEFAULT 0,
    evaluation_outcome TEXT NOT NULL,
    message_date DATETIME NOT NULL,
    analyzed_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_debug_messages_analyzed_at ON debug_analyzed_messages(analyzed_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_debug_messages_outcome ON debug_analyzed_messages(evaluation_outcome, analyzed_at DESC);
CREATE INDEX IF NOT EXISTS idx_debug_messages_channel ON debug_analyzed_messages(channel_id, analyzed_at DESC);
CREATE INDEX IF NOT EXISTS idx_debug_messages_prices_count ON debug_analyzed_messages(prices_found_count, analyzed_at DESC);

CREATE VIEW IF NOT EXISTS v_debug_coupon_false_negatives AS
SELECT *
FROM debug_analyzed_messages
WHERE (has_coupon_indicator = 1 OR tg_entities_json LIKE '%"type":"code"%' OR tg_entities_json LIKE '%"type":"pre"%')
  AND extracted_coupons_count = 0;

CREATE VIEW IF NOT EXISTS v_debug_multi_price_ambiguity AS
SELECT id, channel_id, channel_title, prices_found_count, extracted_lowest_price, all_prices_json, raw_text, tg_entities_json, message_date, analyzed_at
FROM debug_analyzed_messages
WHERE prices_found_count > 1;

CREATE VIEW IF NOT EXISTS v_debug_unrecognized_stores AS
SELECT id, channel_id, channel_title, identified_store, first_url, tg_webpage_url, plain_urls_json, tg_inline_buttons_json, raw_text, message_date, analyzed_at
FROM debug_analyzed_messages
WHERE (first_url != '' OR tg_webpage_url != '' OR tg_inline_buttons_json != '[]')
  AND identified_store = 'Oferta';

CREATE VIEW IF NOT EXISTS v_debug_hidden_metadata_links AS
SELECT id, channel_id, channel_title, first_url, tg_webpage_url, tg_entities_json, tg_inline_buttons_json, raw_text, message_date, analyzed_at
FROM debug_analyzed_messages
WHERE first_url = ''
  AND (tg_entities_json LIKE '%"url":"http%' OR tg_inline_buttons_json LIKE '%"url":"http%' OR tg_webpage_url != '');
