CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    ntfy_topic TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

CREATE TABLE IF NOT EXISTS telegram_credentials (
    id INTEGER PRIMARY KEY,
    encrypted_api_id BLOB NOT NULL,
    encrypted_api_hash BLOB NOT NULL,
    encrypted_session BLOB NOT NULL,
    phone_masked TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS telegram_channels (
    chat_id INTEGER PRIMARY KEY,
    access_hash INTEGER NOT NULL,
    username TEXT NOT NULL,
    title TEXT NOT NULL,
    monitor_products BOOLEAN NOT NULL DEFAULT 0,
    monitor_coupons BOOLEAN NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS user_interests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    term TEXT NOT NULL,
    positive_tokens TEXT NOT NULL,
    negative_tokens TEXT NOT NULL,
    max_price REAL NULL,
    active BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_user_interests_user_id ON user_interests(user_id);
CREATE INDEX IF NOT EXISTS idx_user_interests_active ON user_interests(active);

CREATE TABLE IF NOT EXISTS matched_products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    interest_id INTEGER NULL REFERENCES user_interests(id) ON DELETE SET NULL,
    channel_id INTEGER NOT NULL,
    extracted_price REAL NULL,
    message_text TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_matched_products_user_id ON matched_products(user_id);

CREATE TABLE IF NOT EXISTS coupons (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL,
    store_name TEXT NOT NULL,
    store_url TEXT NOT NULL,
    channel_id INTEGER NOT NULL,
    message_text TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_coupons_code ON coupons(code);
