-- What a pair is about, as the origin side last had it: the console lists
-- pairs by title without asking either forge. Empty until the pair is next
-- reconciled.
ALTER TABLE issue_mapping
    ADD COLUMN title  text NOT NULL DEFAULT '',
    ADD COLUMN author text NOT NULL DEFAULT '';
