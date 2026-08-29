-- История boot_id не даёт старому процессу снова стать текущим после того,
-- как хаб уже принял отчёт от следующего запуска.
CREATE TABLE agent_boots (
    agent_id      varchar(128) NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    boot_id       varchar(128) NOT NULL,
    first_seen_at timestamptz NOT NULL,
    PRIMARY KEY (agent_id, boot_id)
);

INSERT INTO agent_boots (agent_id, boot_id, first_seen_at)
SELECT agent_id, boot_id, COALESCE(last_report_at, now())
FROM agent_status WHERE boot_id IS NOT NULL;

-- Для уже работающего на момент миграции boot сохраняем приближённое начало.
-- Это не даёт неизвестному старому boot представиться новым сразу после deploy.
UPDATE agent_status
SET details = jsonb_set(
    details,
    '{bootStartedAt}',
    to_jsonb((COALESCE(last_report_at, now()) - make_interval(secs => COALESCE(uptime_seconds, 0)::int))::text)
)
WHERE boot_id IS NOT NULL AND NOT (details ? 'bootStartedAt');
