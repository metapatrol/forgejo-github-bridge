-- Pull requests are pairs like issues: both forges number them from the
-- issue sequence, and their conversations are issue comments on both sides.
-- kind tells the two apart.
ALTER TABLE issue_mapping
    ADD COLUMN kind text NOT NULL DEFAULT 'issue' CHECK (kind IN ('issue', 'pull'));

-- What a pull request pair has on top of an issue pair.
CREATE TABLE pull_request_state (
    issue_mapping_id    bigint      PRIMARY KEY REFERENCES issue_mapping (id) ON DELETE CASCADE,
    -- The branch in the contrib repository the head is pushed to.
    contrib_branch      text        NOT NULL,
    -- What the bridge last pushed there. A branch anywhere else was moved by
    -- someone else, and the bridge stops touching it.
    imported_head_sha   text        NOT NULL,
    state               text        NOT NULL CHECK (state IN ('open', 'merged', 'closed')),
    head_frozen         boolean     NOT NULL DEFAULT false,
    touches_ci          boolean     NOT NULL DEFAULT false,
    -- The one GitHub comment that asks for a rebase, edited in place.
    rebase_comment_id   bigint,
    -- Whether the end of the pull request has been reported on GitHub.
    outcome_reported    boolean     NOT NULL DEFAULT false,
    updated_at          timestamptz NOT NULL DEFAULT now()
);
