package api

import "net/http"

// listMandates is GET /mandates (OPEN-12): the government-mandate view —
// exactly GET /campaigns?tag=mandate, the standard campaign list envelope,
// nothing bespoke (07 §5.6). A campaign is a mandate iff it carries the
// literal tag "mandate"; legal/citation copy is frontend content, not API
// data.
func (s *Server) listMandates(w http.ResponseWriter, r *http.Request) {
	s.serveCampaignList(w, r, "mandate")
}
