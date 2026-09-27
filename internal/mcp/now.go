package mcp

import (
	"strconv"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// nowArgs is get_now's arguments: an optional project filter, like get_board.
type nowArgs struct {
	projectRefArg
}

// nowActive is a ticket in progress, waiting on the person or in review, as
// get_now answers it: enough for an orchestrator to see what's moving, not
// the description or subtask list get_ticket would cost a second call for.
// Its status is left out: the inProgress, waiting or inReview group it is
// in already says it, and its
// project prefix is left out too: key already starts with it.
type nowActive struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	// Subtasks is done/total (e.g. "2/5"); left out when the ticket has none.
	Subtasks string `json:"subtasks,omitempty"`
	// ReviewRounds counts the ticket's entries into agent_review; left out
	// when it has never been in review.
	ReviewRounds int `json:"reviewRounds,omitempty"`
	// Review is one of models.NowReviewRunning, NowReviewApproved or
	// NowReviewChanges, set only on a ticket in agent_review.
	Review string `json:"review,omitempty"`
	// Since is truncated to whole seconds: how long the ticket has been in
	// its status needs no finer precision.
	Since time.Time `json:"since"`
}

// nowLanded is a ticket that recently landed, as get_now answers it: its
// commit shas, without the repo each one landed in. Its project prefix is
// left out: key already starts with it.
type nowLanded struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	// DoneAt is truncated to whole seconds; see nowActive.Since.
	DoneAt time.Time `json:"doneAt"`
	// Shas are the commits the ticket landed as, in order; left out when
	// none were recorded.
	Shas []string `json:"shas,omitempty"`
}

// nowAnswer is get_now's answer: the same groups as Store.Now, compacted to
// what checking a run's state needs. Its lists are never nil, so an empty
// board answers with [] rather than null (ACP-148).
type nowAnswer struct {
	InProgress []nowActive `json:"inProgress"`
	Waiting    []nowActive `json:"waiting"`
	InReview   []nowActive `json:"inReview"`
	Landed     []nowLanded `json:"landed"`
}

// getNow answers get_now from Store.Now directly: no second query path, the
// same one request the web UI's Now page makes, compacted for an agent.
func (s *MCPServer) getNow(a nowArgs) (nowAnswer, error) {
	n, err := s.store.Now(a.projectRef(), time.Now())
	if err != nil {
		return nowAnswer{}, err
	}
	return compactNow(n), nil
}

// compactNow drops what get_now leaves out of Store.Now's answer: ids,
// descriptions, subtask lists, each landed commit's repo, status and
// project prefix (both redundant, see nowActive and nowLanded), and
// sub-second timestamp precision.
func compactNow(n *models.Now) nowAnswer {
	out := nowAnswer{
		InProgress: make([]nowActive, len(n.InProgress)),
		Waiting:    make([]nowActive, len(n.Waiting)),
		InReview:   make([]nowActive, len(n.InReview)),
		Landed:     make([]nowLanded, len(n.Landed)),
	}
	for i, t := range n.InProgress {
		out.InProgress[i] = compactNowActive(t)
	}
	for i, t := range n.Waiting {
		out.Waiting[i] = compactNowActive(t)
	}
	for i, t := range n.InReview {
		out.InReview[i] = compactNowActive(t)
	}
	for i, t := range n.Landed {
		out.Landed[i] = compactNowLanded(t)
	}
	return out
}

func compactNowActive(t models.NowTicket) nowActive {
	a := nowActive{
		Key:          t.Key,
		Title:        t.Title,
		ReviewRounds: t.ReviewRounds,
		Review:       t.Review,
		Since:        t.Since.Truncate(time.Second),
	}
	if t.SubtasksTotal > 0 {
		a.Subtasks = strconv.Itoa(t.SubtasksDone) + "/" + strconv.Itoa(t.SubtasksTotal)
	}
	return a
}

func compactNowLanded(t models.LandedTicket) nowLanded {
	l := nowLanded{
		Key:    t.Key,
		Title:  t.Title,
		DoneAt: t.DoneAt.Truncate(time.Second),
	}
	for _, c := range t.Commits {
		l.Shas = append(l.Shas, c.SHA)
	}
	return l
}
