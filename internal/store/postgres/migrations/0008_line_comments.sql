-- Line comments of Forgejo reviews are paired with GitHub review comments
-- (docs/design.md §11). GitHub numbers review comments apart from
-- conversation comments, so a GitHub ID is unique only together with whether
-- it is a review comment; kind says which a pair is.
--
--   comment          conversation comments, and Forgejo review summaries
--   line             a Forgejo line comment and a GitHub review comment
--   line_as_comment  a Forgejo line comment GitHub would not place on its
--                    line, and the conversation comment that quotes it
ALTER TABLE comment_mapping
    ADD COLUMN kind text NOT NULL DEFAULT 'comment'
        CHECK (kind IN ('comment', 'line', 'line_as_comment'));

ALTER TABLE comment_mapping DROP CONSTRAINT comment_mapping_github_comment_id_key;
CREATE UNIQUE INDEX comment_mapping_github_comment ON comment_mapping ((kind = 'line'), github_comment_id);
