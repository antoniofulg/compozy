package convergence

import (
	"errors"
	"testing"
)

const fpA = FindingFingerprint("finding-a")

func reviewActor() Actor { return Actor{Kind: ActorReview, ID: "rev-1", CurrentReview: true} }
func userActor() Actor   { return Actor{Kind: ActorUser, ID: "user-1", RunAuthority: true} }

func TestObservationProjection(t *testing.T) {
	t.Parallel()
	t.Run("Should project ordered observations to current state", func(t *testing.T) {
		t.Parallel()
		p := NewFindingProjection()
		mustObserve(t, p, ObservationEvent{ObservationID: "o1", Fingerprint: fpA, Sequence: 1,
			SnapshotSeq: 1, Severity: SeverityHigh, Outcome: ObservationActionable})
		mustObserve(t, p, ObservationEvent{ObservationID: "o2", Fingerprint: fpA, Sequence: 2,
			SnapshotSeq: 2, Severity: SeverityHigh, Outcome: ObservationResolved})
		if state, _ := p.State(fpA); state != FindingResolved {
			t.Fatalf("expected resolved, got %q", state)
		}
		if len(p.History(fpA)) != 2 {
			t.Fatalf("expected two preserved observations, got %d", len(p.History(fpA)))
		}
	})
	t.Run("Should deduplicate a replayed observation", func(t *testing.T) {
		t.Parallel()
		p := NewFindingProjection()
		mustObserve(t, p, ObservationEvent{ObservationID: "o1", Fingerprint: fpA, Sequence: 1,
			SnapshotSeq: 1, Outcome: ObservationActionable})
		applied, err := p.ApplyObservation(ObservationEvent{ObservationID: "o1", Fingerprint: fpA,
			Sequence: 1, SnapshotSeq: 1, Outcome: ObservationResolved})
		if err != nil || applied {
			t.Fatalf("expected idempotent replay, got applied=%v err=%v", applied, err)
		}
		if state, _ := p.State(fpA); state != FindingActionable {
			t.Fatalf("replay must not change state, got %q", state)
		}
	})
	t.Run("Should reject a stale observation update", func(t *testing.T) {
		t.Parallel()
		p := NewFindingProjection()
		mustObserve(t, p, ObservationEvent{ObservationID: "o2", Fingerprint: fpA, Sequence: 2,
			SnapshotSeq: 5, Outcome: ObservationActionable})
		_, err := p.ApplyObservation(ObservationEvent{ObservationID: "o-old", Fingerprint: fpA,
			Sequence: 3, SnapshotSeq: 4, Outcome: ObservationResolved})
		if !errors.Is(err, ErrObservationStale) {
			t.Fatalf("expected stale rejection, got %v", err)
		}
	})
}

func TestDispositionAuthority(t *testing.T) {
	t.Parallel()
	t.Run("Should allow evidence-backed and authorized dispositions", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name  string
			event DispositionEvent
			want  FindingState
		}{
			{"invalid", DispositionEvent{DecisionID: "d1", Fingerprint: fpA, Type: DispositionInvalid,
				Actor: reviewActor(), Evidence: "no such call path", SnapshotSeq: 1}, FindingInvalid},
			{"duplicate", DispositionEvent{DecisionID: "d1", Fingerprint: fpA, Type: DispositionDuplicate,
				Actor: reviewActor(), Evidence: "same as finding-b", SnapshotSeq: 1}, FindingDuplicate},
			{"waived", DispositionEvent{DecisionID: "d1", Fingerprint: fpA, Type: DispositionWaived,
				Actor: userActor(), Reason: "accepted risk", SnapshotSeq: 1}, FindingWaived},
		}
		for _, tc := range cases {
			t.Run("Should record "+tc.name, func(t *testing.T) {
				t.Parallel()
				p := seededProjection(t)
				if _, err := p.ApplyDisposition(tc.event); err != nil {
					t.Fatalf("apply %s: %v", tc.name, err)
				}
				if state, _ := p.State(fpA); state != tc.want {
					t.Fatalf("expected %q, got %q", tc.want, state)
				}
			})
		}
	})
	rejections := map[string]struct {
		event DispositionEvent
		want  error
	}{
		"invalid without evidence": {DispositionEvent{DecisionID: "d", Fingerprint: fpA,
			Type: DispositionInvalid, Actor: reviewActor(), SnapshotSeq: 1}, ErrDispositionIncomplete},
		"invalid by non-current review": {DispositionEvent{DecisionID: "d", Fingerprint: fpA,
			Type: DispositionInvalid, Actor: Actor{Kind: ActorReview}, Evidence: "x", SnapshotSeq: 1},
			ErrDispositionUnauthorized},
		"model waiver": {DispositionEvent{DecisionID: "d", Fingerprint: fpA, Type: DispositionWaived,
			Actor: Actor{Kind: ActorModel}, Reason: "trust me", SnapshotSeq: 1}, ErrDispositionUnauthorized},
		"unauthorized user waiver": {DispositionEvent{DecisionID: "d", Fingerprint: fpA, Type: DispositionWaived,
			Actor: Actor{Kind: ActorUser}, Reason: "x", SnapshotSeq: 1}, ErrDispositionUnauthorized},
		"waiver without reason": {DispositionEvent{DecisionID: "d", Fingerprint: fpA, Type: DispositionWaived,
			Actor: userActor(), SnapshotSeq: 1}, ErrDispositionIncomplete},
		"stale snapshot": {DispositionEvent{DecisionID: "d", Fingerprint: fpA, Type: DispositionInvalid,
			Actor: reviewActor(), Evidence: "x", SnapshotSeq: 0}, ErrObservationStale},
	}
	for name, tc := range rejections {
		t.Run("Should reject "+name, func(t *testing.T) {
			t.Parallel()
			p := seededProjection(t)
			if _, err := p.ApplyDisposition(tc.event); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v for %s, got %v", tc.want, name, err)
			}
			if state, _ := p.State(fpA); state != FindingActionable {
				t.Fatalf("rejected disposition must leave state unchanged, got %q", state)
			}
		})
	}
}

func seededProjection(t *testing.T) *FindingProjection {
	t.Helper()
	p := NewFindingProjection()
	mustObserve(t, p, ObservationEvent{ObservationID: "seed", Fingerprint: fpA, Sequence: 1,
		SnapshotSeq: 1, Severity: SeverityHigh, Outcome: ObservationActionable})
	return p
}

func mustObserve(t *testing.T, p *FindingProjection, event ObservationEvent) {
	t.Helper()
	if _, err := p.ApplyObservation(event); err != nil {
		t.Fatalf("apply observation %s: %v", event.ObservationID, err)
	}
}
