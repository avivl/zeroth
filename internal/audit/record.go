package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store"
)

// hashVersion is mixed into every digest so an encoding change is a new
// chain rather than a silent reinterpretation of historical records.
const hashVersion = "zeroth-audit-v1"

const (
	ActionAgentCreate    = "agent.create"
	ActionAgentUpdate    = "agent.update"
	ActionAgentRotateKey = "agent.rotate_key"
	ActionRunCreate      = "run.create"
	ActionPlanApprove    = "plan.approve"
	ActionPlanApply      = "plan.apply"
	ActionPlanCrossExam  = "plan.cross_exam"
	ActionPlanBranch     = "plan.branch"
	ActionMemoryWrite    = "memory.write"
	ActionMemoryPropose  = "memory.propose"
	ActionMemoryAccept   = "memory.propose.accept"
	ActionMemoryReject   = "memory.propose.reject"
	ActionMemoryDelete   = "memory.delete"
	ActionCheckpoint     = "checkpoint.create"
	ActionCheckpointRest = "checkpoint.restore"

	ApproverOperator = "operator"
)

// Payload is the signed body of one audit record. Identity fields (id,
// resource type) are not mixed in: they are lookup keys, not claims.
type Payload struct {
	Action        string
	Target        string
	PlanHash      string
	Precondition  string
	Postcondition string
	LeaseID       string
	Approver      string
	AgentPubKey   string
	PrevHash      string
	Timestamp     time.Time
}

// Canonical returns the stable encoding of p. Signatures and chain hashes
// are computed over this, not over JSON.
func Canonical(p Payload) []byte {
	var buf bytes.Buffer
	writeStr(&buf, hashVersion)
	writeStr(&buf, p.Action)
	writeStr(&buf, p.Target)
	writeStr(&buf, p.PlanHash)
	writeStr(&buf, p.Precondition)
	writeStr(&buf, p.Postcondition)
	writeStr(&buf, p.LeaseID)
	writeStr(&buf, p.Approver)
	writeStr(&buf, p.AgentPubKey)
	writeStr(&buf, p.PrevHash)
	writeI64(&buf, unixNanoUTC(p.Timestamp))
	return buf.Bytes()
}

// Digest is SHA-256 of Canonical(p). It is the 32-byte BIP-340 message.
func Digest(p Payload) [32]byte {
	return sha256.Sum256(Canonical(p))
}

// ChainHash is SHA-256 of the canonical payload plus the signature. The
// next record's PrevHash must equal this hex encoding.
func ChainHash(p Payload, sig signer.Signature) string {
	sum := sha256.Sum256(append(Canonical(p), sig[:]...))
	return hex.EncodeToString(sum[:])
}

func payloadFrom(r store.AuditRecord) Payload {
	return Payload{
		Action:        r.Action,
		Target:        r.Target,
		PlanHash:      r.PlanHash,
		Precondition:  r.Precondition,
		Postcondition: r.Postcondition,
		LeaseID:       r.LeaseID.String(),
		Approver:      r.Approver,
		AgentPubKey:   r.AgentPubKey,
		PrevHash:      r.PrevHash,
		Timestamp:     r.CreatedAt,
	}
}

func unixNanoUTC(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
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

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("audit %s: %w", op, err)
}
