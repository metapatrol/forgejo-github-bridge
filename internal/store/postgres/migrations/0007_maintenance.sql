-- The database maintenance rounds the active instance runs (Maintain), so
-- that a console on any instance shows when the last one ran, what it did
-- and when the next is due. Only the newest rounds are kept.
CREATE TABLE maintenance_round (
    id           bigserial   PRIMARY KEY,
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    tables       text[]      NOT NULL DEFAULT '{}',
    rebuilt      text[]      NOT NULL DEFAULT '{}',
    dropped      text[]      NOT NULL DEFAULT '{}',
    failures     text[]      NOT NULL DEFAULT '{}'
);

CREATE OR REPLACE FUNCTION forgejo_github_bridge_event() RETURNS trigger AS $$
DECLARE
    r record;
    payload json;
BEGIN
    IF TG_OP = 'DELETE' THEN r := OLD; ELSE r := NEW; END IF;
    -- One branch per table: a record's fields are resolved when the
    -- statement naming them runs, so no statement may name another table's.
    IF TG_TABLE_NAME = 'issue_mapping' THEN
        payload := json_build_object('kind', 'pair', 'op', lower(TG_OP),
            'id', r.id, 'repository', r.repository, 'pairKind', r.kind);
    ELSIF TG_TABLE_NAME IN ('pull_request_state', 'comment_mapping') THEN
        payload := json_build_object('kind', 'pair', 'op', 'update', 'id', r.issue_mapping_id);
    ELSIF TG_TABLE_NAME = 'dirty' THEN
        payload := json_build_object('kind', 'queue', 'op', lower(TG_OP),
            'repository', r.repository, 'side', r.side, 'number', r.number);
    ELSIF TG_TABLE_NAME = 'github_rate_limit' THEN
        payload := json_build_object('kind', 'rateLimit', 'op', lower(TG_OP), 'resource', r.resource);
    ELSIF TG_TABLE_NAME = 'maintenance_round' THEN
        payload := json_build_object('kind', 'maintenance', 'op', lower(TG_OP), 'id', r.id,
            'finished', r.finished_at IS NOT NULL);
    ELSE
        payload := json_build_object('kind', 'cursor', 'op', lower(TG_OP),
            'repository', r.repository, 'side', r.side, 'stream', r.stream);
    END IF;
    PERFORM pg_notify('forgejo_github_bridge_events', payload::text);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER maintenance_round_event AFTER INSERT OR UPDATE ON maintenance_round
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
