import { useEffect, useState, type ReactNode } from "react";
import { Check, Copy, ExternalLink, Folder, GitBranch, GitCommitHorizontal, GitPullRequest } from "lucide-react";
import type { Delivery } from "../api/client";
import { groupByRepo, hasDelivery, prLabel } from "../lib/delivery";

const SECTION_HEADING = "text-[11px] font-semibold uppercase tracking-wider text-slate-500 mb-2.5";
const BOX = "divide-y divide-slate-800 rounded-md border border-slate-800 bg-slate-900/60";
const ICON = "h-3.5 w-3.5 shrink-0 text-slate-500";

/** How many characters of a sha the section shows; the full sha is on hover and is what Copy copies. */
const SHORT_SHA = 7;

/**
 * The editor's read-only Delivery section: where the ticket's work lives
 * (branch, worktree, pull request) and the commits it landed as, grouped by
 * repo. Agents set these through MCP, the CLI or the API; the editor only
 * shows them. Nothing renders when no field is set.
 */
export default function DeliverySection({ delivery }: { delivery?: Delivery }) {
  if (!hasDelivery(delivery)) return null;
  const { branch, worktree, prUrl } = delivery;
  const commits = delivery.landedCommits ?? [];
  const groups = groupByRepo(commits);

  return (
    <div data-testid="delivery-section">
      <h3 className={SECTION_HEADING}>Delivery</h3>
      {(branch || worktree || prUrl) && (
        <div className={BOX}>
          {branch && (
            <Row icon={<GitBranch aria-hidden="true" className={ICON} />} label="Branch">
              <span title={branch} className="min-w-0 flex-1 truncate font-mono text-xs text-slate-300">
                {branch}
              </span>
              <CopyButton value={branch} label="Copy branch" />
            </Row>
          )}
          {worktree && (
            <Row icon={<Folder aria-hidden="true" className={ICON} />} label="Worktree">
              {/* A long path keeps its end in view: the ellipsis goes at the start. */}
              <span
                title={worktree}
                data-testid="delivery-worktree"
                className="min-w-0 flex-1 truncate text-left font-mono text-xs text-slate-300 [direction:rtl]"
              >
                {`‎${worktree}‎`}
              </span>
              <CopyButton value={worktree} label="Copy worktree path" />
            </Row>
          )}
          {prUrl && (
            <Row icon={<GitPullRequest aria-hidden="true" className={ICON} />} label="PR">
              <a
                href={prUrl}
                target="_blank"
                rel="noreferrer"
                title={prUrl}
                className="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-blue-400 hover:underline"
              >
                <span className="min-w-0 flex-1 truncate">{prLabel(prUrl)}</span>
                <ExternalLink aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-slate-600" />
              </a>
            </Row>
          )}
        </div>
      )}
      {commits.length > 0 && (
        <>
          <div className={`mb-1.5 text-xs font-medium text-slate-500 ${branch || worktree || prUrl ? "mt-3.5" : ""}`}>
            Landed commits{" "}
            <span className="text-slate-600">
              · {commits.length} in {groups.length} {groups.length === 1 ? "repo" : "repos"}
            </span>
          </div>
          <ul className={BOX} aria-label="Landed commits">
            {groups.map(([repo, list]) => (
              <li key={repo} className="px-2.5 py-1.5">
                <div className="mb-1 truncate font-mono text-[11px] text-slate-500">{repo}</div>
                <ul aria-label={`Commits in ${repo}`} className="flex flex-wrap gap-1.5">
                  {list.map((c) => (
                    <li
                      key={c.sha}
                      className="inline-flex items-center gap-1 rounded-md border border-slate-700 bg-slate-800 px-1.5 py-0.5 font-mono text-xs text-slate-300"
                    >
                      <GitCommitHorizontal aria-hidden="true" className="h-3 w-3 shrink-0 text-slate-500" />
                      <span title={c.sha}>{c.sha.slice(0, SHORT_SHA)}</span>
                      <CopyButton value={c.sha} label={`Copy commit ${c.sha.slice(0, SHORT_SHA)}`} />
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        </>
      )}
      <p className="mt-1.5 text-[11px] text-slate-600">Read only. Agents set these through MCP, the CLI or the API.</p>
    </div>
  );
}

function Row({ icon, label, children }: { icon: ReactNode; label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 items-center gap-2 px-2.5 py-1.5">
      {icon}
      <span className="w-[62px] shrink-0 text-xs text-slate-500">{label}</span>
      {children}
    </div>
  );
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);

  const copy = () => {
    navigator.clipboard
      ?.writeText(value)
      .then(() => setCopied(true))
      .catch(() => {
        // Clipboard access can be refused; the text stays selectable.
      });
  };

  return (
    <button
      type="button"
      aria-label={copied ? "Copied" : label}
      title={copied ? "Copied" : label}
      onClick={copy}
      className="shrink-0 text-slate-600 transition-colors hover:text-slate-300"
    >
      {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
    </button>
  );
}
