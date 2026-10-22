CREATE TABLE IF NOT EXISTS wishes (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    wisher     VARCHAR(20) NOT NULL,
    wish       VARCHAR(30) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_wishes_created_at (created_at DESC),
    CONSTRAINT chk_wishes_wisher_not_empty CHECK (CHAR_LENGTH(TRIM(wisher)) > 0),
    CONSTRAINT chk_wishes_wish_not_empty CHECK (CHAR_LENGTH(TRIM(wish)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
