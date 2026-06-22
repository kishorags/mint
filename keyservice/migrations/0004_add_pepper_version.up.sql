-- Add pepper_version to api_keys so we can rotate the HMAC pepper without
-- invalidating all existing key hashes at once. Keys created before this
-- migration default to version 1 (the original pepper).
ALTER TABLE api_keys
  ADD COLUMN pepper_version INTEGER NOT NULL DEFAULT 1;
