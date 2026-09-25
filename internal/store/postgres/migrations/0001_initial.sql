-- The pairs of issues the bridge keeps in step.
--
-- Each number appears once per repository and side: an issue has at most one
-- counterpart, and the unique constraints are what make creating a second one
-- fail instead of quietly duplicating a conversation.
CREATE TABLE issue_mapping (
    id              bigserial   PRIMARY KEY,
    repository      text        NOT NULL,
    github_number   bigint      NOT NULL,
    forgejo_number  bigint      NOT NULL,
    origin          text        NOT NULL CHECK (origin IN ('github', 'forgejo')),
    synced_closed   boolean     NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repository, github_number),
    UNIQUE (repository, forgejo_number)
);

-- The pairs of comments on mapped issues. Comment IDs are unique across a
-- whole forge, not per issue, so the constraints are global.
CREATE TABLE comment_mapping (
    id                  bigserial   PRIMARY KEY,
    issue_mapping_id    bigint      NOT NULL REFERENCES issue_mapping (id) ON DELETE CASCADE,
    github_comment_id   bigint      NOT NULL UNIQUE,
    forgejo_comment_id  bigint      NOT NULL UNIQUE,
    origin              text        NOT NULL CHECK (origin IN ('github', 'forgejo')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX comment_mapping_issue ON comment_mapping (issue_mapping_id);

-- How far each poll has read. The cursor is a timestamp the forge itself
-- reported, never this host's clock.
CREATE TABLE poll_cursor (
    repository  text        NOT NULL,
    side        text        NOT NULL CHECK (side IN ('github', 'forgejo')),
    stream      text        NOT NULL,
    cursor      timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, side, stream)
);

-- Resources waiting to be reconciled, one row per resource however many
-- times it changed: marking an entry that exists only moves marked_at.
--
-- clock_timestamp() rather than now(): two marks in one transaction must
-- still differ, or clearing after a reconcile could remove a mark that
-- arrived while it ran.
CREATE TABLE dirty (
    repository  text        NOT NULL,
    side        text        NOT NULL CHECK (side IN ('github', 'forgejo')),
    number      bigint      NOT NULL,
    marked_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    not_before  timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempts    integer     NOT NULL DEFAULT 0,
    last_error  text,
    PRIMARY KEY (repository, side, number)
);

CREATE INDEX dirty_due ON dirty (not_before);
