-- Панель показывает постоянную ссылку подписки, поэтому токен и приватный
-- recovery-ключ теперь хранятся восстановимо — зашифрованными мастер-ключом,
-- как приватные ключи пользователей. token_hash остаётся: по нему идёт резолв.
-- Столбцы nullable: у подписок, выпущенных до этой миграции, секретов нет,
-- их ссылку по-прежнему можно получить только ротацией.
ALTER TABLE subscriptions
    ADD COLUMN token_ciphertext            bytea,
    ADD COLUMN token_nonce                 bytea,
    ADD COLUMN token_key_id                text,
    ADD COLUMN recovery_private_ciphertext bytea,
    ADD COLUMN recovery_private_nonce      bytea,
    ADD COLUMN recovery_private_key_id     text;

ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_secrets_complete CHECK (
    (token_ciphertext IS NULL AND token_nonce IS NULL AND token_key_id IS NULL)
    OR (token_ciphertext IS NOT NULL AND token_nonce IS NOT NULL AND token_key_id IS NOT NULL)
);

ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_recovery_secrets_complete CHECK (
    (recovery_private_ciphertext IS NULL AND recovery_private_nonce IS NULL AND recovery_private_key_id IS NULL)
    OR (recovery_private_ciphertext IS NOT NULL AND recovery_private_nonce IS NOT NULL AND recovery_private_key_id IS NOT NULL)
);
