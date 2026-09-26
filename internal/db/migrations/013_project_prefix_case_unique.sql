-- Makes project prefixes unique ignoring letter case. projects.prefix was
-- UNIQUE only under SQLite's binary collation, so "GLOW" and "glow" could be
-- two projects, while every prefix resolver matches with LOWER(prefix) =
-- LOWER(?). The index below uses the same LOWER(), so what it forbids is
-- exactly what the resolvers would find ambiguous. The store checks the same
-- rule first to give a clear error (checkPrefixFree); this index is the
-- guarantee underneath it.
--
-- A database that already holds such a pair cannot take the index, and this
-- migration never picks a winner: renaming or merging a project would change
-- its ticket keys, which people and agents have written down. So it stops
-- first, naming every colliding project, and nothing is changed: the runner
-- rolls the whole file back and leaves 013 unapplied, so it runs again on the
-- next start once the prefixes have been made distinct by hand (all but one
-- project in each group renamed; the message says so, and that a renamed
-- project's ticket keys change with it).
--
-- SQLite only lets RAISE() be called from a trigger, so the check is a
-- temporary trigger on a temporary table, fired by one insert. RAISE with an
-- expression as its message needs SQLite 3.47, and group_concat's ORDER BY
-- 3.44; the bundled modernc.org/sqlite is newer than both. Temporary objects
-- live on this connection only and are dropped again below.
CREATE TEMP TABLE prefix_case_check (x INTEGER);

CREATE TEMP TRIGGER prefix_case_check_raise BEFORE INSERT ON prefix_case_check
WHEN EXISTS (SELECT 1 FROM main.projects GROUP BY LOWER(prefix) HAVING COUNT(*) > 1)
BEGIN
	SELECT RAISE(ABORT,
		'project prefixes that differ only by letter case: ' ||
		(SELECT group_concat(clash, '; ' ORDER BY folded) FROM (
			SELECT LOWER(prefix) AS folded,
				group_concat(prefix || ' ("' || name || '", id ' || id || ')', ' and ' ORDER BY prefix, id) AS clash
			FROM main.projects
			GROUP BY LOWER(prefix)
			HAVING COUNT(*) > 1
		)) ||
		'. Nothing was changed. In each group, keep one project''s prefix and give every other project a new one; ' ||
		'a renamed project''s ticket keys change with its prefix. For example: ' ||
		'sqlite3 <db> "UPDATE projects SET prefix = ''NEW'' WHERE id = ''<id>''", then start taskboard again.');
END;

INSERT INTO prefix_case_check VALUES (1);

DROP TRIGGER prefix_case_check_raise;
DROP TABLE prefix_case_check;

CREATE UNIQUE INDEX idx_projects_prefix_lower ON projects (LOWER(prefix));
