import type { Delivery, LandedCommit } from "../api/client";

/** Whether a ticket's delivery has anything to show. */
export function hasDelivery(d: Delivery | undefined): d is Delivery {
  return !!d && !!(d.branch || d.worktree || d.prUrl || d.landedCommits?.length);
}

/** A GitHub pull request url as owner/repo#N; any other url as it is. */
export function prLabel(url: string): string {
  const m = url.match(/^https?:\/\/(?:www\.)?github\.com\/([^/]+\/[^/]+)\/pull\/(\d+)/);
  return m ? `${m[1]}#${m[2]}` : url;
}

/** Landed commits grouped by repo, repos in the order they first appear, commits in the order given. */
export function groupByRepo(commits: LandedCommit[]): [string, LandedCommit[]][] {
  const groups = new Map<string, LandedCommit[]>();
  for (const c of commits) {
    const group = groups.get(c.repo);
    if (group) group.push(c);
    else groups.set(c.repo, [c]);
  }
  return [...groups];
}
