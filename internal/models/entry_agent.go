package models

// EntryAgent is an agent that wrote an entry or handled a note, with its
// session: what an entry's author line shows (role, model and provider; the
// resume command and the machine). A read of entries over HTTP carries these
// beside the page, keyed by agent id, so each agent is sent once however many
// entries it wrote.
type EntryAgent struct {
	Agent
	Session Session `json:"session"`
}
