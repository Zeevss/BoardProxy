-- Панель выдаёт лимиты пользователя вместе с периодом сброса и политикой при достижении.
-- Периоды дополняются недельным и «без сброса», политики — сбросом счётчика.
ALTER TABLE user_traffic_quotas DROP CONSTRAINT user_traffic_quotas_period_check;
ALTER TABLE user_traffic_quotas ADD CONSTRAINT user_traffic_quotas_period_check
    CHECK (period IN ('daily', 'weekly', 'monthly', 'none'));

ALTER TABLE user_traffic_quotas DROP CONSTRAINT user_traffic_quotas_action_check;
ALTER TABLE user_traffic_quotas ADD CONSTRAINT user_traffic_quotas_action_check
    CHECK (action IN ('alert', 'reset', 'disable'));

-- Политика reset обслуживает пользователя дальше, начиная отсчёт заново.
-- NULL означает, что счётчик идёт с начала календарного периода.
ALTER TABLE user_traffic_quotas ADD COLUMN counter_start timestamptz;
