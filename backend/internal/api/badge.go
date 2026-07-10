package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/whynoipv6/internal/domain"
)

// badgeVariant is one row of the normative copy/color table (07 §5.2) — the
// ONE place to reword. Copy is public status vocabulary, never ladder
// branding: a README badge never says "sinner"/"hero".
type badgeVariant struct {
	Message  string // shields message ("supported", "no IPv6", …)
	Color    string // shields color name (.json variant)
	Hex      string // SVG fill
	TextFill string // message text fill (the gold accent)
	IsError  bool   // shields renders unknown as a genuine error
}

var badgeVariants = map[string]badgeVariant{
	"supported": {Message: "supported", Color: "brightgreen", Hex: "#4c1", TextFill: "#fff"},
	"gold":      {Message: "gold", Color: "brightgreen", Hex: "#4c1", TextFill: "#ffd700"},
	"partial":   {Message: "partial", Color: "yellow", Hex: "#dfb317", TextFill: "#fff"},
	"no-ipv6":   {Message: "no IPv6", Color: "red", Hex: "#e05d44", TextFill: "#fff"},
	"inactive":  {Message: "inactive", Color: "lightgrey", Hex: "#9f9f9f", TextFill: "#fff"},
	"unknown":   {Message: "unknown", Color: "lightgrey", Hex: "#9f9f9f", TextFill: "#fff", IsError: true},
}

// pickBadgeVariant applies the first-match rule: no row / disabled /
// unknown → unknown; hero+gold → gold; hero → supported; partial; sinner →
// no IPv6; inactive.
func pickBadgeVariant(found bool, classification string, gold, disabled bool) string {
	switch {
	case !found || disabled || classification == "unknown":
		return "unknown"
	case classification == "hero" && gold:
		return "gold"
	case classification == "hero":
		return "supported"
	case classification == "partial":
		return "partial"
	case classification == "sinner":
		return "no-ipv6"
	default: // inactive
		return "inactive"
	}
}

// Fixed shields-flat geometry: no font measurement, no dependencies —
// byte-deterministic per variant (07 §5.2). The label box is constant; the
// message box derives from a per-character width table.
const (
	badgeLabelWidth = 43 // the "IPv6" box
	badgeLabelText  = 330
)

func badgeMessageWidth(msg string) int { return 6*len(msg) + 14 }

// RenderBadgeSVG is the pure (variant) → []byte renderer, golden-file
// pinned (10-testing §7.6). No host label rides in the SVG — the three
// unknown inputs must render byte-identically.
func RenderBadgeSVG(variantName string) []byte {
	v := badgeVariants[variantName]
	rw := badgeMessageWidth(v.Message)
	w := badgeLabelWidth + rw
	mx := (badgeLabelWidth*2 + rw) * 5 // midpoint of the message box, ×10
	mtl := (rw - 14) * 10
	return []byte(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="IPv6: %s">`+
			`<title>IPv6: %s</title>`+
			`<linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>`+
			`<clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>`+
			`<g clip-path="url(#r)">`+
			`<rect width="%d" height="20" fill="#555"/>`+
			`<rect x="%d" width="%d" height="20" fill="%s"/>`+
			`<rect width="%d" height="20" fill="url(#s)"/>`+
			`</g>`+
			`<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110">`+
			`<text aria-hidden="true" x="225" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" textLength="%d">IPv6</text>`+
			`<text x="225" y="140" transform="scale(.1)" textLength="%d">IPv6</text>`+
			`<text aria-hidden="true" x="%d" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" textLength="%d">%s</text>`+
			`<text x="%d" y="140" fill="%s" transform="scale(.1)" textLength="%d">%s</text>`+
			`</g></svg>`+"\n",
		w, v.Message,
		v.Message,
		w,
		badgeLabelWidth,
		badgeLabelWidth, rw, v.Hex,
		w,
		badgeLabelText,
		badgeLabelText,
		mx, mtl, v.Message,
		mx, v.TextFill, mtl, v.Message,
	))
}

// reservedTLDs is the badge/live-check reserved-TLD policy layer (07 §2.8).
var reservedTLDs = map[string]bool{
	"test": true, "example": true, "invalid": true,
	"localhost": true, "internal": true, "local": true,
}

func reservedTLD(host string) bool {
	i := strings.LastIndexByte(host, '.')
	return reservedTLDs[host[i+1:]]
}

// getBadge dispatches /badge/{host}.svg|.json. The suffix is part of the
// route contract: a suffix-less path is a route-miss 404, never a variant.
func (s *Server) getBadge(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	switch {
	case strings.HasSuffix(file, ".svg"):
		s.getBadgeSVG(w, r, strings.TrimSuffix(file, ".svg"))
	case strings.HasSuffix(file, ".json"):
		s.getBadgeJSON(w, r, strings.TrimSuffix(file, ".json"))
	default:
		NotFound(w, r, "Not found", "Badges are served as /badge/{host}.svg or /badge/{host}.json.")
	}
}

// badgeHost applies the badge failure policy: invalid → 400
// invalid-parameter (the declared exception — a malformed embed is not a
// legitimate request), valid → always renderable.
func (s *Server) badgeHost(w http.ResponseWriter, r *http.Request, raw string) (string, bool) {
	host, err := domain.Canonicalize(raw)
	if err != nil || reservedTLD(host) {
		InvalidParameter(w, r, "The badge host is not a valid public domain name.")
		return "", false
	}
	return host, true
}

// badgeVariantFor runs the read-only lookup: never inserts, never enqueues,
// never touches last_requested_at (07 §5.2).
func (s *Server) badgeVariantFor(r *http.Request, host string) (string, error) {
	row, err := s.svc.Q.BadgeDomain(r.Context(), host)
	if errors.Is(err, pgx.ErrNoRows) {
		return pickBadgeVariant(false, "", false, false), nil
	}
	if err != nil {
		return "", err
	}
	return pickBadgeVariant(true, string(row.Classification), row.Gold, row.Disabled), nil
}

func (s *Server) badgeCache(w http.ResponseWriter, r *http.Request) bool {
	generation, _, err := s.svc.Generation(r.Context())
	if err != nil {
		return false // serve uncached rather than fail the embed
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	return applyETag(w, r, fmt.Sprintf(`"b%d-%s"`, generation, queryFingerprint(r)))
}

// getBadgeSVG is GET /badge/{host}.svg.
func (s *Server) getBadgeSVG(w http.ResponseWriter, r *http.Request, raw string) {
	host, ok := s.badgeHost(w, r, raw)
	if !ok {
		return
	}
	variant, err := s.badgeVariantFor(r, host)
	if err != nil {
		InternalError(w, r)
		return
	}
	if s.badgeCache(w, r) {
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(RenderBadgeSVG(variant))
}

// shieldsEndpoint is the shields.io endpoint schema — the one sanctioned
// camelCase exception (07 §2.3).
type shieldsEndpoint struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
	CacheSeconds  int    `json:"cacheSeconds"`
	IsError       bool   `json:"isError"`
}

// getBadgeJSON is GET /badge/{host}.json.
func (s *Server) getBadgeJSON(w http.ResponseWriter, r *http.Request, raw string) {
	host, ok := s.badgeHost(w, r, raw)
	if !ok {
		return
	}
	variant, err := s.badgeVariantFor(r, host)
	if err != nil {
		InternalError(w, r)
		return
	}
	if s.badgeCache(w, r) {
		return
	}
	v := badgeVariants[variant]
	WriteJSON(w, http.StatusOK, shieldsEndpoint{
		SchemaVersion: 1, Label: "IPv6", Message: v.Message, Color: v.Color,
		CacheSeconds: 86400, IsError: v.IsError,
	})
}
