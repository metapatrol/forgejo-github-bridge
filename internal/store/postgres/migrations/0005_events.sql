-- Every change to what the console shows is announced on one channel, so that
-- live views follow without polling (docs/console.md). A console on any
-- instance hears it: the announcement goes through the database, not through
-- the process that made the change.
--
-- The payload names what changed, never its content. NOTIFY is delivered on
-- commit, and not at all for a transaction that rolls back.
CREATE FUNCTION forgejo_github_bridge_event() RETURNS trigger AS $$
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
    ELSE
        payload := json_build_object('kind', 'cursor', 'op', lower(TG_OP),
            'repository', r.repository, 'side', r.side, 'stream', r.stream);
    END IF;
    PERFORM pg_notify('forgejo_github_bridge_events', payload::text);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER issue_mapping_event AFTER INSERT OR UPDATE OR DELETE ON issue_mapping
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
CREATE TRIGGER pull_request_state_event AFTER INSERT OR UPDATE ON pull_request_state
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
CREATE TRIGGER comment_mapping_event AFTER INSERT OR UPDATE OR DELETE ON comment_mapping
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
CREATE TRIGGER dirty_event AFTER INSERT OR UPDATE OR DELETE ON dirty
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
CREATE TRIGGER poll_cursor_event AFTER INSERT OR UPDATE ON poll_cursor
    FOR EACH ROW EXECUTE FUNCTION forgejo_github_bridge_event();
