-- +goose Up
-- Currencies: how each one is written (symbol, where it goes, decimals,
-- separators). Amounts everywhere are integers in the currency's minor
-- units; changing the default changes the label, never the amounts. One
-- row is the default: the currency of new payments and of plan prices.
CREATE TABLE currencies (
    id                  bigserial PRIMARY KEY,
    code                text NOT NULL,
    name                text NOT NULL,
    symbol              text NOT NULL,
    symbol_first        boolean NOT NULL DEFAULT true,
    precision           smallint NOT NULL DEFAULT 2,
    decimal_mark        text NOT NULL DEFAULT '.',
    thousands_separator text NOT NULL DEFAULT ',',
    is_enabled          boolean NOT NULL DEFAULT true,
    is_default          boolean NOT NULL DEFAULT false,
    created_by          bigint,
    updated_by          bigint,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT currencies_code_check CHECK (code ~ '^[A-Z]{3}$'),
    CONSTRAINT currencies_precision_check CHECK (precision BETWEEN 0 AND 4),
    CONSTRAINT currencies_default_enabled_check CHECK (NOT is_default OR is_enabled)
);

CREATE UNIQUE INDEX currencies_code_unique ON currencies (code);
CREATE UNIQUE INDEX currencies_one_default ON currencies (is_default) WHERE is_default;

INSERT INTO currencies (code, name, symbol, symbol_first, precision, decimal_mark, thousands_separator) VALUES
    ('USD', 'US Dollar', '$', true, 2, '.', ','),
    ('BDT', 'Bangladeshi Taka', '৳', true, 2, '.', ','),
    ('EUR', 'Euro', '€', true, 2, ',', '.');

-- An existing install keeps its money: a currency its payments use gets a
-- row, and the most used one becomes the default. A new install starts on
-- US dollars.
INSERT INTO currencies (code, name, symbol)
SELECT DISTINCT currency, currency, currency
FROM payments
WHERE currency ~ '^[A-Z]{3}$' AND currency NOT IN (SELECT code FROM currencies);

UPDATE currencies SET is_default = true
WHERE code = COALESCE(
    (SELECT currency FROM payments WHERE currency ~ '^[A-Z]{3}$'
     GROUP BY currency ORDER BY count(*) DESC, currency LIMIT 1),
    'USD');

ALTER TABLE payments ADD CONSTRAINT payments_currency_check CHECK (currency ~ '^[A-Z]{3}$') NOT VALID;

-- +goose Down
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_currency_check;
DROP TABLE IF EXISTS currencies;
