package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Expect is an optimistic version guard for a managed file (方案 §15.2
// 乐观并发控制/版本号): a Put only applies while the file still matches it.
type Expect struct {
	kind expectKind
	hash [sha256.Size]byte
}

type expectKind int

const (
	expectAny expectKind = iota
	expectAbsent
	expectHash
)

// ExpectAny stages a Put without a version check.
func ExpectAny() Expect { return Expect{} }

// ExpectAbsent requires the target file to be missing.
func ExpectAbsent() Expect { return Expect{kind: expectAbsent} }

// ExpectHash requires the target file to have exactly this content hash.
func ExpectHash(h [sha256.Size]byte) Expect { return Expect{kind: expectHash, hash: h} }

// HashBytes returns the version tag used for content.
func HashBytes(data []byte) [sha256.Size]byte { return sha256.Sum256(data) }

func (e Expect) String() string {
	switch e.kind {
	case expectAbsent:
		return "absent"
	case expectHash:
		return "sha256:" + hex.EncodeToString(e.hash[:])
	default:
		return "any"
	}
}

func (e Expect) satisfiedBy(data []byte, exists bool) bool {
	switch e.kind {
	case expectAbsent:
		return !exists
	case expectHash:
		return exists && sha256.Sum256(data) == e.hash
	default:
		return true
	}
}

// parseExpect restores a guard from its journal representation.
func parseExpect(s string) (Expect, error) {
	switch {
	case s == "any":
		return ExpectAny(), nil
	case s == "absent":
		return ExpectAbsent(), nil
	case strings.HasPrefix(s, "sha256:"):
		raw, err := hex.DecodeString(strings.TrimPrefix(s, "sha256:"))
		if err != nil || len(raw) != sha256.Size {
			return Expect{}, fmt.Errorf("invalid expect %q", s)
		}
		var h [sha256.Size]byte
		copy(h[:], raw)
		return ExpectHash(h), nil
	default:
		return Expect{}, fmt.Errorf("invalid expect %q", s)
	}
}

// Operation kinds and transaction stages recorded on disk and used by tests.
const (
	opKindPut           = "put"
	opKindAppend        = "append_jsonl"
	stageJournalDurable = "journal-durable"
	stageCommitted      = "committed"
	stageCleaned        = "cleaned"
)

func stageApplied(i int) string { return fmt.Sprintf("applied:%d", i) }

// txnStageHook, when non-nil, runs after each durable transaction stage. Tests
// point it at a process exit to simulate an interruption at an exact point;
// production leaves it nil.
var txnStageHook func(stage string)

func txnStage(stage string) {
	if txnStageHook != nil {
		txnStageHook(stage)
	}
}

// op is one staged change.
type op struct {
	kind    string
	rel     string
	abs     string
	expect  Expect
	payload []byte
	// preSize is the target size observed under the lock; appends must start
	// exactly there (recorded in the journal for idempotent replay).
	preSize int64
}

// Tx collects the changes of a single transaction. Ops are validated and
// applied under the project lock by Store.Write.
type Tx struct {
	s    *Store
	ops  []op
	seen map[string]bool
}

// Put stages a replacement of rel with content, guarded by expect.
func (tx *Tx) Put(rel string, content []byte, expect Expect) error {
	abs, err := tx.s.resolve(rel)
	if err != nil {
		return err
	}
	if err := tx.claim(rel, abs); err != nil {
		return err
	}
	data := append([]byte(nil), content...)
	tx.ops = append(tx.ops, op{kind: opKindPut, rel: rel, abs: abs, expect: expect, payload: data})
	return nil
}

// PutYAML serializes v with EncodeYAML and stages it as a replacement of rel.
func (tx *Tx) PutYAML(rel string, v any, expect Expect) error {
	data, err := EncodeYAML(v)
	if err != nil {
		return err
	}
	return tx.Put(rel, data, expect)
}

// AppendJSONL stages record as one JSONL line appended to rel. Appends are
// serialized by the project lock, so they never conflict.
func (tx *Tx) AppendJSONL(rel string, record any) error {
	data, err := MarshalJSONL(record)
	if err != nil {
		return err
	}
	abs, err := tx.s.resolve(rel)
	if err != nil {
		return err
	}
	if err := tx.claim(rel, abs); err != nil {
		return err
	}
	tx.ops = append(tx.ops, op{kind: opKindAppend, rel: rel, abs: abs, payload: data})
	return nil
}

func (tx *Tx) claim(rel, abs string) error {
	if tx.seen[abs] {
		return fmt.Errorf("storage: %s is staged twice in one transaction", rel)
	}
	tx.seen[abs] = true
	return nil
}

// journal is the transaction intent written before the commit point.
type journal struct {
	TxnID     string      `yaml:"txn_id"`
	CreatedAt string      `yaml:"created_at"`
	PID       int         `yaml:"pid"`
	Ops       []journalOp `yaml:"ops"`
}

type journalOp struct {
	Kind          string `yaml:"kind"`
	Rel           string `yaml:"rel"`
	Expect        string `yaml:"expect,omitempty"`
	Payload       string `yaml:"payload"`
	PayloadSHA256 string `yaml:"payload_sha256"`
	PayloadSize   int64  `yaml:"payload_size"`
	PreSize       int64  `yaml:"pre_size,omitempty"`
}

// commitMarker is the commit point: its atomic arrival means the transaction
// will be replayed to completion after any interruption.
type commitMarker struct {
	TxnID         string `yaml:"txn_id"`
	CommittedAt   string `yaml:"committed_at"`
	JournalSHA256 string `yaml:"journal_sha256"`
	OpCount       int    `yaml:"op_count"`
}

func (j *journal) validate() error {
	if len(j.Ops) == 0 {
		return fmt.Errorf("journal %s has no ops", j.TxnID)
	}
	for i := range j.Ops {
		o := &j.Ops[i]
		if o.Rel == "" || o.Payload == "" || o.PayloadSHA256 == "" || o.PayloadSize < 0 {
			return fmt.Errorf("journal %s op %d is incomplete", j.TxnID, i)
		}
		if _, err := payloadRel(o.Payload); err != nil {
			return fmt.Errorf("journal %s op %d: %v", j.TxnID, i, err)
		}
		switch o.Kind {
		case opKindPut:
			if _, err := parseExpect(o.Expect); err != nil {
				return fmt.Errorf("journal %s op %d: %v", j.TxnID, i, err)
			}
		case opKindAppend:
			if o.PreSize < 0 {
				return fmt.Errorf("journal %s op %d: negative pre_size", j.TxnID, i)
			}
		default:
			return fmt.Errorf("journal %s op %d: unknown kind %q", j.TxnID, i, o.Kind)
		}
	}
	return nil
}

// payloadRel validates a journal payload reference ("payload/000.bin").
func payloadRel(name string) (string, error) {
	if path.IsAbs(name) || strings.Contains(name, `\`) {
		return "", fmt.Errorf("%w: payload %q", ErrUnsafePath, name)
	}
	clean := path.Clean(name)
	if !strings.HasPrefix(clean, payloadDirName+"/") || strings.Contains(clean, "..") {
		return "", fmt.Errorf("%w: payload %q", ErrUnsafePath, name)
	}
	return clean, nil
}

// commitLocked runs the write protocol described in the M0.3 plan:
// re-check preconditions under the lock, persist payloads and the journal,
// publish the commit marker (the commit point), apply every op with fsync,
// then remove the transaction directory. The callers holds the exclusive lock.
func (s *Store) commitLocked(tx *Tx) error {
	for i := range tx.ops {
		o := &tx.ops[i]
		switch o.kind {
		case opKindPut:
			cur, exists, err := readFileMaybe(o.abs)
			if err != nil {
				return fmt.Errorf("read %s: %w", o.rel, err)
			}
			if !o.expect.satisfiedBy(cur, exists) {
				return fmt.Errorf("%w: %s: expected %s, found %s", ErrConflict, o.rel, o.expect, describeCurrent(cur, exists))
			}
		case opKindAppend:
			tail, err := InspectJSONL(o.abs)
			if err != nil {
				return err
			}
			if !tail.Complete {
				return fmt.Errorf("%w: %s: %d bytes without line terminator; repair the file before appending", ErrIncompleteTail, o.rel, tail.Size)
			}
			o.preSize = tail.Size
		}
	}

	id, err := s.newTxnID()
	if err != nil {
		return err
	}
	dir := filepath.Join(s.txnDir, id)
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(payloadDirName)), 0o755); err != nil {
		return fmt.Errorf("create transaction directory: %w", err)
	}

	j := journal{TxnID: id, CreatedAt: s.opts.Now().UTC().Format(time.RFC3339Nano), PID: os.Getpid()}
	for i := range tx.ops {
		o := &tx.ops[i]
		rel := fmt.Sprintf("%s/%03d.bin", payloadDirName, i)
		if err := WriteFileSync(filepath.Join(dir, filepath.FromSlash(rel)), o.payload, 0o644); err != nil {
			return err
		}
		j.Ops = append(j.Ops, journalOp{
			Kind:          o.kind,
			Rel:           o.rel,
			Expect:        expectField(o),
			Payload:       rel,
			PayloadSHA256: sha256Hex(o.payload),
			PayloadSize:   int64(len(o.payload)),
			PreSize:       o.preSize,
		})
	}
	journalBytes, err := EncodeYAML(j)
	if err != nil {
		return err
	}
	if err := AtomicWrite(filepath.Join(dir, journalFileName), journalBytes, 0o644); err != nil {
		return err
	}
	txnStage(stageJournalDurable)

	marker := commitMarker{
		TxnID:         id,
		CommittedAt:   s.opts.Now().UTC().Format(time.RFC3339Nano),
		JournalSHA256: sha256Hex(journalBytes),
		OpCount:       len(j.Ops),
	}
	markerBytes, err := EncodeYAML(marker)
	if err != nil {
		return err
	}
	if err := AtomicWrite(filepath.Join(dir, commitFileName), markerBytes, 0o644); err != nil {
		return err
	}
	txnStage(stageCommitted)

	for i := range tx.ops {
		if err := applyPayload(tx.ops[i].abs, tx.ops[i].kind, tx.ops[i].payload, tx.ops[i].preSize); err != nil {
			return err
		}
		txnStage(stageApplied(i))
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clean up transaction %s: %w", id, err)
	}
	_ = fsyncDir(s.txnDir)
	txnStage(stageCleaned)
	return nil
}

// expectField records the guard for Put ops; appends carry none.
func expectField(o *op) string {
	if o.kind == opKindPut {
		return o.expect.String()
	}
	return ""
}

// applyPayload performs one staged change on its target.
func applyPayload(abs, kind string, payload []byte, preSize int64) error {
	switch kind {
	case opKindPut:
		return AtomicWrite(abs, payload, 0o644)
	case opKindAppend:
		return appendJSONL(abs, payload, preSize)
	default:
		return fmt.Errorf("unknown op kind %q", kind)
	}
}

func (s *Store) newTxnID() (string, error) {
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", fmt.Errorf("generate transaction id: %w", err)
	}
	return fmt.Sprintf("%s-%d-%s",
		s.opts.Now().UTC().Format("20060102T150405.000000000Z"), os.Getpid(), hex.EncodeToString(rnd[:])), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// describeCurrent renders a short version tag for error messages.
func describeCurrent(data []byte, exists bool) string {
	if !exists {
		return "absent"
	}
	return "sha256:" + sha256Hex(data)[:12]
}
