-- +goose Up

-- Typed agent questions: a question carries a kind (free-text / pick-one /
-- confirm) and, for pick-one, the offered options, so the client can render a
-- typed prompt instead of a bare text box. Persisted alongside pending_question
-- so a snapshot refetch (seq-gap reconcile, reconnect) reconstructs the typed
-- prompt rather than degrading it to free text. Empty kind ('') means free-text
-- input — the backward-compatible default for questions that predate this
-- column or omit the kind.
ALTER TABLE tasks ADD COLUMN pending_question_kind    TEXT   NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN pending_question_options TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE tasks DROP COLUMN pending_question_options;
ALTER TABLE tasks DROP COLUMN pending_question_kind;