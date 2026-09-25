-- Whether the one rebase comment currently asks for a rebase, or says the
-- pull request is up to date. A pull request that falls behind again after
-- being up to date gets a new comment rather than an edit, because an edit
-- notifies nobody.
ALTER TABLE pull_request_state
    ADD COLUMN rebase_requested boolean NOT NULL DEFAULT false;

UPDATE pull_request_state SET rebase_requested = (rebase_comment_id IS NOT NULL);
