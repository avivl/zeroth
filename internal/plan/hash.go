package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"time"

	"github.com/avivl/zeroth/internal/policy"
)

// hashVersion is mixed into every digest so an encoding change is a new
// hash (and therefore a new approval) rather than a silent reinterpretation.
const hashVersion = "zeroth-plan-v1"

// HashOf returns the canonical hash of p's rows and draft constraints.
// Identity, status, review metadata, and timestamps are excluded: approve
// gates this bundle, it does not rewrite it. Independent rows (different
// targets) are sorted by target so a permutation of those rows is the same
// plan. Rows that share a target keep their relative order, because create
// then modify is not modify then create.
func HashOf(p Plan) policy.PlanHash {
	rows := canonicalRows(p.Rows)
	creds := canonicalCreds(p.Credentials)

	var buf bytes.Buffer
	writeStr(&buf, hashVersion)
	writeStr(&buf, p.Summary)
	writeI64(&buf, unixNanoUTC(p.ExpiresAt))
	writeI64(&buf, p.CostCeiling)
	writeStr(&buf, string(p.Scope))
	writeU32(&buf, uint32(len(creds)))
	for _, c := range creds {
		writeStr(&buf, c.Provider)
		writeStr(&buf, c.Kind)
	}
	writeU32(&buf, uint32(len(rows)))
	for _, r := range rows {
		writeStr(&buf, string(r.Op))
		writeStr(&buf, r.Target)
		writeStr(&buf, r.Payload)
		writeStr(&buf, string(r.Lease))
		writeStr(&buf, r.Precondition)
		writeStr(&buf, r.IdempotencyKey)
		writeStr(&buf, r.Postcondition)
		writeStr(&buf, r.CostEstimate)
	}
	sum := sha256.Sum256(buf.Bytes())
	return policy.PlanHash(hex.EncodeToString(sum[:]))
}

func unixNanoUTC(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func canonicalRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Target < out[j].Target
	})
	return out
}

func canonicalCreds(creds []Credential) []Credential {
	out := make([]Credential, len(creds))
	copy(out, creds)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func writeU32(buf *bytes.Buffer, n uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	buf.Write(b[:])
}

func writeI64(buf *bytes.Buffer, n int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	buf.Write(b[:])
}

func writeStr(buf *bytes.Buffer, s string) {
	writeU32(buf, uint32(len(s)))
	buf.WriteString(s)
}
