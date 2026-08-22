package audit

import (
	"fmt"

	"github.com/avivl/zeroth/internal/signer"
	"github.com/avivl/zeroth/internal/store"
)

// Failure names a verification error and the record that failed. Callers
// print Record so an auditor can find the row without trusting Zeroth.
type Failure struct {
	Record store.AuditID
	Prev   store.AuditID
	Err    error
}

func (f Failure) Error() string {
	if f.Record.IsZero() {
		return f.Err.Error()
	}
	if !f.Prev.IsZero() {
		return fmt.Sprintf("audit verify: record %s: %v (after %s)", f.Record.String(), f.Err, f.Prev.String())
	}
	return fmt.Sprintf("audit verify: record %s: %v", f.Record.String(), f.Err)
}

func (f Failure) Unwrap() error { return f.Err }

// VerifyRecord checks one record's Schnorr signature against the agent's
// registered pubkeys. It does not walk the chain.
func VerifyRecord(r store.AuditRecord, keys []store.AgentKey) error {
	if err := checkRegistered(r, keys); err != nil {
		return Failure{Record: r.ID, Err: err}
	}
	pub, err := signer.ParsePublicKey(r.AgentPubKey)
	if err != nil {
		return Failure{Record: r.ID, Err: fmt.Errorf("public key: %w", err)}
	}
	sig, err := signer.ParseSignature(r.Signature)
	if err != nil {
		return Failure{Record: r.ID, Err: fmt.Errorf("signature: %w", err)}
	}
	p := payloadFrom(r)
	dig := Digest(p)
	if err := signer.Verify(pub, dig[:], sig); err != nil {
		return Failure{Record: r.ID, Err: fmt.Errorf("signature invalid")}
	}
	want := ChainHash(p, sig)
	if r.Hash != want {
		return Failure{Record: r.ID, Err: fmt.Errorf("content hash mismatch")}
	}
	return nil
}

// VerifyChain checks signatures, registry membership, and prev_hash links.
// records must be oldest-first. Tampering names the record; a missing
// middle row names the first record whose predecessor does not match.
func VerifyChain(records []store.AuditRecord, keys []store.AgentKey) error {
	prev := ""
	var prevID store.AuditID
	for i, r := range records {
		if r.PrevHash != prev {
			err := fmt.Errorf("chain broken: prev_hash does not match")
			if i == 0 {
				err = fmt.Errorf("chain broken: first record has leftover prev_hash (missing predecessor)")
			}
			return Failure{Record: r.ID, Prev: prevID, Err: err}
		}
		if err := VerifyRecord(r, keys); err != nil {
			return err
		}
		prev = r.Hash
		prevID = r.ID
	}
	return nil
}

func checkRegistered(r store.AuditRecord, keys []store.AgentKey) error {
	if r.AgentPubKey == "" {
		return fmt.Errorf("missing agent pubkey")
	}
	for _, k := range keys {
		if k.PubKey != r.AgentPubKey {
			continue
		}
		if r.AgentID.IsZero() || k.AgentID == r.AgentID {
			return nil
		}
	}
	return fmt.Errorf("agent pubkey is not in the registry")
}
