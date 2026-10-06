-- What the bridge wrote, one row per write, newest kept for a while
-- (BRIDGE_ACTIVITY_RETENTION): the console's answer to "what did the bridge
-- do today". It names what was written where, never its content.
--
-- No foreign key to the pair: a pair may be forgotten, its history stays.
CREATE TABLE activity (
    id              bigserial   PRIMARY KEY,
    at              timestamptz NOT NULL DEFAULT now(),
    repository      text        NOT NULL,
    pair_id         bigint,
    side            text        NOT NULL CHECK (side IN ('github', 'forgejo')),
    kind            text        NOT NULL,
    action          text        NOT NULL,
    github_number   bigint,
    forgejo_number  bigint
);

CREATE INDEX activity_at ON activity (at DESC, id DESC);
CREATE INDEX activity_pair ON activity (pair_id) WHERE pair_id IS NOT NULL;
CREATE INDEX activity_repository ON activity (repository, at DESC);

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
    ELSIF TG_TABLE_NAME = 'activity' THEN
        payload := json_build_object('kind', 'activity', 'op', lower(TG_OP), 'id', r.id,
            'repository', r.repository, 'pairId', r.pair_id);
    ELSE
        payload := json_build_object('kind', 'cursor', 'op', lower(TG_OP),
            'repository', r.repository, 'side', r.side, 'stream', r.stream);
    END IF;
    PERFORM pg_notify('forgejo_github_bridge_events', payload::text);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Inserts only: pruning old rows is not news.
CREATE TRIGGER activity_event AFTER INSERT ON activity
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
