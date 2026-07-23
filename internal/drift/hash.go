package drift

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Hash is a stable digest of the drift set: sorted address|attribute|after
// tuples. Two runs seeing the same drift produce the same hash, which is how duplicate PRs are detected.
func (p *Plan) Hash() (string, error) {
	var lines []string
	for _, r := range p.ResourceDrift {
		if r.Deleted() {
			lines = append(lines, r.Address+"|deleted")
			continue
		}
		attrs, err := r.ChangedAttrs()
		if err != nil {
			return "", err
		}
		for _, a := range attrs {
			lines = append(lines, fmt.Sprintf("%s|%s|%s", r.Address, a.Attribute, a.After))
		}
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
