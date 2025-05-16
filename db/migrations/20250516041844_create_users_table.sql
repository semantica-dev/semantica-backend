-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),             -- Primary key, unique identifier for the user (UUID)
    username VARCHAR(255) UNIQUE NOT NULL,                     -- Unique username for login
    email VARCHAR(255) UNIQUE NOT NULL,                        -- Unique email, can also be used for login
    hashed_password TEXT NOT NULL,                             -- Hashed password for the user
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),             -- Timestamp of when the user was created
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()              -- Timestamp of when the user was last updated
);

COMMENT ON TABLE users IS 'Stores user account information';

-- +goose Down
DROP TABLE IF EXISTS users;