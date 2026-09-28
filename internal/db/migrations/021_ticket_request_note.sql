-- An optional note the person leaves alongside their answer, whether it is
-- a free-text or matched-choice answer to a question or approved/declined
-- to an approval. It is set together with the answer in AnswerRequest, so
-- it is never set before answered_at, but it stays optional even then: a
-- blank note is stored as NULL, not ''. It comes back on every read of the
-- request, the same as the answer itself.
ALTER TABLE ticket_requests ADD COLUMN note TEXT;
