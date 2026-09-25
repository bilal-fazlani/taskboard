-- Rewrites every created_at/updated_at value into one fixed-width UTC
-- format: "YYYY-MM-DDTHH:MM:SS.fffffffffZ". Go's %.9f-style formatting
-- trims trailing zeros, so two values with a different number of fractional
-- digits sort by their trailing punctuation, not by time; a local offset
-- (or one row stamped in UTC, another local, or either side of a DST
-- change) makes a plain ORDER BY or MAX() wrong too, since the offset and
-- zone name come after the part that actually orders the row. Every value
-- this format writes is the same length and already in UTC, so comparing
-- the stored text compares the instants.
--
-- Two shapes are recognised and converted, each guarded so it only fires on
-- text that actually matches it (see below); anything else, including a
-- shape this migration does not know, is left exactly as it was rather than
-- guessed at:
--
--  A. Go's time.Time.String(), the only shape this app has ever written:
--     "YYYY-MM-DD HH:MM:SS[.fraction] ±HHMM ZONE", optionally with a
--     trailing " m=±D.dddddddd" monotonic-clock reading (stripped first).
--     The offset is the " +HHMM"/" -HHMM" that follows the date and time —
--     dates never contain a space before their own hyphens, so it is
--     unambiguous — turned into "+HH:MM"/"-HH:MM" for SQLite's date
--     functions; a row with no offset (bare CURRENT_TIMESTAMP text) is
--     treated as already UTC.
--  B. ISO 8601: "YYYY-MM-DD[T ]HH:MM:SS[.fraction]" followed by "Z" or a
--     "±HH:MM" offset with a colon. Not written by this app, but a
--     plausible shape for a hand-edited or externally written row, and
--     SQLite's date functions read it natively.
--
-- Before either shape trusts its extracted pieces, it checks with GLOB that
-- the base date/time is exactly the expected digits-and-punctuation shape
-- and that whatever it read as the fraction is all digits and nothing else.
-- Skipping that check was the bug an earlier version of this migration had:
-- an ISO value's own "+01:00"/"Z" offset was read as part of the fraction
-- (having no space in front of it to signal otherwise), producing unreadable
-- text. A value that fails shape A's checks is left for shape B to try; a
-- value that fails both is passed through unchanged by the trailing
-- COALESCE, so nothing is guessed at or corrupted.
--
-- SQLite's date functions have accepted a numeric "±HH:MM" (or "Z") zone
-- suffix on their input, converting it to UTC, for as long as this project
-- has depended on them; nothing here relies on a specific recent version.
-- The offset shift itself is always a whole number of minutes (real-world
-- time zones do not use sub-minute offsets), so it never changes the
-- fractional-second digits, which are carried over from the original text
-- rather than recomputed.

UPDATE projects SET created_at = (
    WITH raw(v) AS (SELECT projects.created_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, projects.created_at)
    FROM resultA, resultB
)
WHERE created_at IS NOT NULL AND created_at != '';

UPDATE projects SET updated_at = (
    WITH raw(v) AS (SELECT projects.updated_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, projects.updated_at)
    FROM resultA, resultB
)
WHERE updated_at IS NOT NULL AND updated_at != '';

UPDATE tickets SET created_at = (
    WITH raw(v) AS (SELECT tickets.created_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, tickets.created_at)
    FROM resultA, resultB
)
WHERE created_at IS NOT NULL AND created_at != '';

UPDATE tickets SET updated_at = (
    WITH raw(v) AS (SELECT tickets.updated_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, tickets.updated_at)
    FROM resultA, resultB
)
WHERE updated_at IS NOT NULL AND updated_at != '';

UPDATE epics SET created_at = (
    WITH raw(v) AS (SELECT epics.created_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, epics.created_at)
    FROM resultA, resultB
)
WHERE created_at IS NOT NULL AND created_at != '';

UPDATE epics SET updated_at = (
    WITH raw(v) AS (SELECT epics.updated_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, epics.updated_at)
    FROM resultA, resultB
)
WHERE updated_at IS NOT NULL AND updated_at != '';

UPDATE documents SET created_at = (
    WITH raw(v) AS (SELECT documents.created_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, documents.created_at)
    FROM resultA, resultB
)
WHERE created_at IS NOT NULL AND created_at != '';

UPDATE documents SET updated_at = (
    WITH raw(v) AS (SELECT documents.updated_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, documents.updated_at)
    FROM resultA, resultB
)
WHERE updated_at IS NOT NULL AND updated_at != '';

UPDATE ticket_status_changes SET created_at = (
    WITH raw(v) AS (SELECT ticket_status_changes.created_at),
         s1(v) AS (
             SELECT CASE WHEN instr(raw.v, ' m=') > 0
                          THEN substr(raw.v, 1, instr(raw.v, ' m=') - 1)
                          ELSE raw.v END
             FROM raw
         ),
         opA(v) AS (
             SELECT CASE WHEN instr(s1.v, ' +') > 0 THEN instr(s1.v, ' +')
                          WHEN instr(s1.v, ' -') > 0 THEN instr(s1.v, ' -')
                          ELSE 0 END
             FROM s1
         ),
         dtA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, 1, opA.v - 1) ELSE s1.v END FROM s1, opA),
         offA(v) AS (SELECT CASE WHEN opA.v > 0 THEN substr(s1.v, opA.v + 1, 5) ELSE '+0000' END FROM s1, opA),
         validA(v) AS (
             SELECT CASE WHEN
                 substr(dtA.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(dtA.v) = 19
                     OR (substr(dtA.v, 20, 1) = '.' AND length(dtA.v) > 20 AND substr(dtA.v, 21) NOT GLOB '*[^0-9]*')
                 )
                 AND (opA.v = 0 OR offA.v GLOB '[+-][0-9][0-9][0-9][0-9]')
             THEN 1 ELSE 0 END
             FROM dtA, opA, offA
         ),
         fpA(v) AS (SELECT instr(dtA.v, '.') FROM dtA),
         baseA(v) AS (SELECT CASE WHEN fpA.v > 0 THEN substr(dtA.v, 1, fpA.v - 1) ELSE dtA.v END FROM dtA, fpA),
         fracA(v) AS (
             SELECT CASE WHEN fpA.v > 0 THEN substr(substr(dtA.v, fpA.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM dtA, fpA
         ),
         offAColon(v) AS (SELECT substr(offA.v, 1, 1) || substr(offA.v, 2, 2) || ':' || substr(offA.v, 4, 2) FROM offA),
         resultA(v) AS (
             SELECT CASE WHEN validA.v = 1
                 THEN replace(datetime(baseA.v || offAColon.v), ' ', 'T') || '.' || fracA.v || 'Z'
                 ELSE NULL END
             FROM validA, baseA, offAColon, fracA
         ),
         coreB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN substr(s1.v, 1, length(s1.v) - 1)
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, 1, length(s1.v) - 6)
                 ELSE NULL END
             FROM s1
         ),
         offB(v) AS (
             SELECT CASE
                 WHEN s1.v GLOB '*[0-9]Z' THEN '+00:00'
                 WHEN s1.v GLOB '*[0-9][+-][0-9][0-9]:[0-9][0-9]' THEN substr(s1.v, length(s1.v) - 5)
                 ELSE NULL END
             FROM s1
         ),
         validB(v) AS (
             SELECT CASE WHEN coreB.v IS NOT NULL AND (
                 substr(coreB.v, 1, 19) GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][T ][0-9][0-9]:[0-9][0-9]:[0-9][0-9]'
                 AND (
                     length(coreB.v) = 19
                     OR (substr(coreB.v, 20, 1) = '.' AND length(coreB.v) > 20 AND substr(coreB.v, 21) NOT GLOB '*[^0-9]*')
                 )
             ) THEN 1 ELSE 0 END
             FROM coreB
         ),
         fpB(v) AS (SELECT instr(coreB.v, '.') FROM coreB),
         baseB(v) AS (SELECT CASE WHEN fpB.v > 0 THEN substr(coreB.v, 1, fpB.v - 1) ELSE coreB.v END FROM coreB, fpB),
         fracB(v) AS (
             SELECT CASE WHEN fpB.v > 0 THEN substr(substr(coreB.v, fpB.v + 1) || '000000000', 1, 9)
                          ELSE '000000000' END
             FROM coreB, fpB
         ),
         resultB(v) AS (
             SELECT CASE WHEN validB.v = 1
                 THEN replace(datetime(baseB.v || offB.v), ' ', 'T') || '.' || fracB.v || 'Z'
                 ELSE NULL END
             FROM validB, baseB, offB, fracB
         )
    SELECT COALESCE(resultA.v, resultB.v, ticket_status_changes.created_at)
    FROM resultA, resultB
)
WHERE created_at IS NOT NULL AND created_at != '';

