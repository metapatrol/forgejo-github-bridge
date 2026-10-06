-- The GitHub rate limit as the active instance last saw it, one row per
-- window ("core" for the REST API), so that a console on any instance can
-- show how much is left. It lives in the leader's memory otherwise.
CREATE TABLE github_rate_limit (
    resource     text        PRIMARY KEY,
    "limit"      integer     NOT NULL,
    remaining    integer     NOT NULL,
    used         integer     NOT NULL,
    reset_at     timestamptz NOT NULL,
    observed_at  timestamptz NOT NULL
);

-- Announced like every other change the console shows (0005).
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
    ELSE
        payload := json_build_object('kind', 'cursor', 'op', lower(TG_OP),
            'repository', r.repository, 'side', r.side, 'stream', r.stream);
    END IF;
    PERFORM pg_notify('forgejo_github_bridge_events', payload::text);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER github_rate_limit_event AFTER INSERT OR UPDATE ON github_rate_limit
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
