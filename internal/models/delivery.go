package models

// Delivery is where a ticket's work lives and where it landed: the branch and
// worktree it is built in, its pull request, and the commits it landed as.
// Commits never name their ticket, so the ticket is the one place that links
// the two, and FindTicketsByCommit reads the link back.
//
// Only the full ticket carries it (get_ticket, GET /api/tickets/{id},
// `ticket get`), and only when at least one field is set; lists leave it out.
type Delivery struct {
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	PRURL    string `json:"prUrl,omitempty"`
	// LandedCommits are the commits the ticket landed as, in the order they
	// were given, each with the repo it landed in, so a ticket that spans
	// several repos keeps them apart.
	LandedCommits []LandedCommit `json:"landedCommits,omitempty"`
}

// LandedCommit is one commit a ticket landed as. SHA is lowercase hex, 7 to
// 64 characters (a short or a full sha). Repo is the repository it landed in,
// a free-form identifier like the ticket's repos (acme/billing-api).
type LandedCommit struct {
	SHA  string `json:"sha"`
	Repo string `json:"repo"`
}

// IsEmpty reports whether no delivery field is set.
func (d Delivery) IsEmpty() bool {
	return d.Branch == "" && d.Worktree == "" && d.PRURL == "" && len(d.LandedCommits) == 0
}

// DeliveryUpdate sets a ticket's delivery fields, as the delivery field of
// UpdateTicketRequest. Every field left out (nil, or JSON null) is unchanged;
// "" clears a text field. LandedCommits replaces the whole list, and an empty
// list clears it. A commit given without a repo takes the ticket's repo when
// the ticket has exactly one, and is an ErrInvalidInput otherwise. On any
// error nothing in the request is applied.
type DeliveryUpdate struct {
	Branch        *string         `json:"branch,omitempty"`
	Worktree      *string         `json:"worktree,omitempty"`
	PRURL         *string         `json:"prUrl,omitempty"`
	LandedCommits *[]LandedCommit `json:"landedCommits,omitempty"`
}

// CommitTicket is a ticket found by one of its landed commits: enough to name
// and open it, and the landed commits of it that matched the sha.
type CommitTicket struct {
	ID      string         `json:"id"`
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Status  string         `json:"status"`
	URL     string         `json:"url,omitempty"`
	Commits []LandedCommit `json:"commits"`

	// ProjectPrefix is how weburl names the ticket in its URL (the key, or
	// the id when the project has no prefix). It is not part of the JSON.
	ProjectPrefix string `json:"-"`
}
