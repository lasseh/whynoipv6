package checker

import "strings"

// sanitizeText strips C0 control bytes and DEL from remote-supplied text.
// A NUL byte survives json.Marshal as a six-byte Unicode escape, which jsonb
// rejects (SQLSTATE 22P05); one such byte anywhere in a Detail fails the whole
// commit batch and the domain's scan is lost. Every string built from bytes a
// remote host chose passes through here first.
func sanitizeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// sanitizeTexts returns a cleaned copy. Callers pass slices owned by parsed
// certificates and DNS messages, so cleaning in place is not an option.
func sanitizeTexts(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = sanitizeText(s)
	}
	return out
}
