CREATE TABLE IF NOT EXISTS payments (
    id SERIAL PRIMARY KEY,
    correlation_id VARCHAR(100) NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    processor VARCHAR(20) NOT NULL,
    requested_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    processed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX CONCURRENTLY idx_payments_window_grp
ON payments (processed_at, processor)
INCLUDE (amount);      